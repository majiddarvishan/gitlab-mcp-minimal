package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

const validJSON = `{
  "base_url": "https://gitlab.example.com/",
  "token_file": "/run/secrets/gitlab_token",
  "projects": [{"id": 123, "path": "example-group/example-project"}]
}`

func parse(t *testing.T, s string) (*Config, error) {
	t.Helper()
	return Parse(strings.NewReader(s))
}

func TestParseValidAppliesDefaults(t *testing.T) {
	c, err := parse(t, validJSON)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://gitlab.example.com" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.BranchPrefix != "agent/" || c.RequestTimeoutSeconds != 30 {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.Limits != defaultLimits {
		t.Errorf("limits = %+v", c.Limits)
	}
	if len(c.WriteDenyPaths) != len(validate.DefaultDenyPaths) {
		t.Errorf("deny paths = %v", c.WriteDenyPaths)
	}
	// The default deny-list must be a copy, not an alias of the package variable.
	c.WriteDenyPaths[0] = "changed"
	if validate.DefaultDenyPaths[0] == "changed" {
		t.Error("DefaultDenyPaths was mutated")
	}
	al, err := c.Allowlist()
	if err != nil {
		t.Fatal(err)
	}
	if p, err := al.Resolve("123"); err != nil || p.Path != "example-group/example-project" {
		t.Errorf("allow-list: %+v %v", p, err)
	}
}

func TestParseOverrides(t *testing.T) {
	c, err := parse(t, `{
	  "base_url": "http://gitlab.internal:8080/gitlab", "allow_insecure_http": true,
	  "token_env": "GITLAB_TOKEN", "ca_file": "/etc/ssl/internal-ca.pem",
	  "projects": [{"id": 1, "path": "a/b"}], "branch_prefix": "bots/x/",
	  "write_deny_paths": [], "request_timeout_seconds": 5,
	  "limits": {"max_files_per_commit": 3, "max_file_bytes": 1000, "max_commit_bytes": 2000}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://gitlab.internal:8080/gitlab" || c.BranchPrefix != "bots/x/" ||
		c.RequestTimeout().Seconds() != 5 || c.Limits.MaxFilesPerCommit != 3 || c.Limits.MaxTraceBytes != defaultLimits.MaxTraceBytes {
		t.Errorf("unexpected config: %+v", c)
	}
	if c.WriteDenyPaths == nil || len(c.WriteDenyPaths) != 0 {
		t.Errorf("explicit empty deny-list must stay empty, got %v", c.WriteDenyPaths)
	}
}

func TestParseRejects(t *testing.T) {
	proj := `"projects": [{"id": 1, "path": "a/b"}]`
	tok := `"token_env": "T"`
	base := `"base_url": "https://g.example.com"`
	cases := map[string]string{
		"empty":               ``,
		"not json":            `nope`,
		"array":               `[]`,
		"unknown field":       `{` + base + `,` + tok + `,` + proj + `, "sse": true}`,
		"trailing object":     `{` + base + `,` + tok + `,` + proj + `} {}`,
		"trailing garbage":    `{` + base + `,` + tok + `,` + proj + `} x`,
		"missing base_url":    `{` + tok + `,` + proj + `}`,
		"http without opt-in": `{"base_url": "http://g.example.com",` + tok + `,` + proj + `}`,
		"ftp":                 `{"base_url": "ftp://g.example.com",` + tok + `,` + proj + `}`,
		"no host":             `{"base_url": "https://",` + tok + `,` + proj + `}`,
		"userinfo":            `{"base_url": "https://u:p@g.example.com",` + tok + `,` + proj + `}`,
		"query":               `{"base_url": "https://g.example.com/?x=1",` + tok + `,` + proj + `}`,
		"fragment":            `{"base_url": "https://g.example.com/#x",` + tok + `,` + proj + `}`,
		"dotdot path":         `{"base_url": "https://g.example.com/a/../b",` + tok + `,` + proj + `}`,
		"encoded path":        `{"base_url": "https://g.example.com/a%2Fb",` + tok + `,` + proj + `}`,
		"no token source":     `{` + base + `,` + proj + `}`,
		"both token sources":  `{` + base + `, "token_file": "/x", "token_env": "T",` + proj + `}`,
		"relative token file": `{` + base + `, "token_file": "token",` + proj + `}`,
		"bad env name":        `{` + base + `, "token_env": "A-B",` + proj + `}`,
		"relative ca":         `{` + base + `,` + tok + `, "ca_file": "ca.pem",` + proj + `}`,
		"no projects":         `{` + base + `,` + tok + `}`,
		"empty projects":      `{` + base + `,` + tok + `, "projects": []}`,
		"bad project path":    `{` + base + `,` + tok + `, "projects": [{"id": 1, "path": "a/../b"}]}`,
		"zero project id":     `{` + base + `,` + tok + `, "projects": [{"id": 0, "path": "a/b"}]}`,
		"duplicate project":   `{` + base + `,` + tok + `, "projects": [{"id": 1, "path": "a/b"},{"id": 2, "path": "A/B"}]}`,
		"bad prefix":          `{` + base + `,` + tok + `,` + proj + `, "branch_prefix": "agent"}`,
		"bad deny entry":      `{` + base + `,` + tok + `,` + proj + `, "write_deny_paths": ["../x"]}`,
		"negative timeout":    `{` + base + `,` + tok + `,` + proj + `, "request_timeout_seconds": -1}`,
		"huge timeout":        `{` + base + `,` + tok + `,` + proj + `, "request_timeout_seconds": 3600}`,
		"negative limit":      `{` + base + `,` + tok + `,` + proj + `, "limits": {"max_file_bytes": -1}}`,
		"limit above ceiling": `{` + base + `,` + tok + `,` + proj + `, "limits": {"max_files_per_commit": 101}}`,
		"file > commit":       `{` + base + `,` + tok + `,` + proj + `, "limits": {"max_file_bytes": 5000, "max_commit_bytes": 1000}}`,
		"oversized input":     `{"base_url": "` + strings.Repeat("a", maxConfigBytes) + `"}`,
	}
	for name, in := range cases {
		if c, err := parse(t, in); err == nil {
			t.Errorf("%s: accepted: %+v", name, c)
		}
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(validJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestLoadToken(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfgFor := func(file string) *Config { return &Config{TokenFile: file} }
	noEnv := func(string) string { return "" }

	good := write("good", "glpat-abc_123-XYZ\n")
	s, err := cfgFor(good).LoadToken(noEnv)
	if err != nil || s.Reveal() != "glpat-abc_123-XYZ" {
		t.Fatalf("good token: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v", s, cfgFor(good)), "glpat-abc") {
		t.Error("token leaked through formatting")
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err == nil {
		if _, err := cfgFor(link).LoadToken(noEnv); err != nil {
			t.Errorf("symlinked secret (as used by Kubernetes) rejected: %v", err)
		}
	}

	bad := map[string]string{
		"empty":       write("empty", ""),
		"whitespace":  write("ws", " \n\t"),
		"inner space": write("space", "glpat abc"),
		"two lines":   write("lines", "glpat-a\nglpat-b"),
		"non-ascii":   write("utf8", "glpat-é"),
		"control":     write("ctl", "glpat-\x01a"),
		"too long":    write("long", strings.Repeat("a", maxTokenBytes+1)),
		"huge file":   write("huge", strings.Repeat("a", maxTokenFile+1)),
		"missing":     filepath.Join(dir, "nope"),
		"directory":   dir,
	}
	for name, p := range bad {
		if _, err := cfgFor(p).LoadToken(noEnv); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "glpat") {
			t.Errorf("%s: error leaks token: %v", name, err)
		}
	}

	env := func(k string) string {
		if k == "GITLAB_TOKEN" {
			return "  glpat-fromenv \n"
		}
		return ""
	}
	if s, err := (&Config{TokenEnv: "GITLAB_TOKEN"}).LoadToken(env); err != nil || s.Reveal() != "glpat-fromenv" {
		t.Errorf("env token: %v", err)
	}
	if _, err := (&Config{TokenEnv: "UNSET_VAR"}).LoadToken(env); err == nil {
		t.Error("unset env var accepted")
	}
}

// The documented example must always parse, so the docs cannot drift from the code.
func TestExampleConfigParses(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Parse(f); err != nil {
		t.Fatalf("config.example.json is invalid: %v", err)
	}
}
