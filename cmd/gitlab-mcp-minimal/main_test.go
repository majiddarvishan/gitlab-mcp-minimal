package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const e2eToken = "glpat-e2e-secret-token"

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gitlab-mcp-minimal")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodConfig = `{
  "base_url": "http://gitlab.internal.invalid:9", "allow_insecure_http": true, "token_env": "GITLAB_TOKEN",
  "projects": [{"id": 42, "path": "example-group/example-project"}]
}`

// The real binary, driven by the official SDK client over stdio: stdout must
// carry only protocol messages and every diagnostic must go to stderr.
func TestBinaryServesOverStdio(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "-config", writeConfig(t, goodConfig))
	cmd.Env = append(os.Environ(), "GITLAB_TOKEN="+e2eToken)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v\nstderr: %s", err, stderr.String())
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatalf("tools: %v %+v", err, tools)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must be read-only", tool.Name)
		}
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects"})
	if err != nil || res.IsError {
		t.Fatalf("call: %v %+v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"example-group/example-project"`) || strings.Contains(string(raw), "internal.invalid") {
		t.Errorf("unexpected result: %s", raw)
	}
	if err := cs.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	log := stderr.String()
	if !strings.Contains(log, `"tool":"list_projects"`) || !strings.Contains(log, `"outcome":"ok"`) {
		t.Errorf("audit line missing from stderr: %q", log)
	}
	if strings.Contains(log, e2eToken) {
		t.Errorf("token leaked to stderr: %q", log)
	}
}

func runFailing(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected failure for %v", args)
	}
	return out.String(), errb.String()
}

func TestStartupFailuresAreQuietOnStdout(t *testing.T) {
	bin := buildBinary(t)
	clean := []string{"PATH=" + os.Getenv("PATH")}
	cases := []struct {
		name, wantErr string
		env           []string
		args          []string
	}{
		{"no configuration", "no configuration", clean, nil},
		{"missing file", "config:", clean, []string{"-config", filepath.Join(t.TempDir(), "missing.json")}},
		{"invalid config", "config:", clean, []string{"-config", writeConfig(t, `{"base_url": "ftp://x"}`)}},
		{"missing token", "token", clean, []string{"-config", writeConfig(t, goodConfig)}},
		{"unknown flag", "flag provided but not defined", clean, []string{"-nope"}},
	}
	for _, c := range cases {
		stdout, stderr := runFailing(t, bin, c.env, c.args...)
		if stdout != "" {
			t.Errorf("%s: stdout must stay empty, got %q", c.name, stdout)
		}
		if !strings.Contains(stderr, c.wantErr) {
			t.Errorf("%s: stderr = %q, want it to mention %q", c.name, stderr, c.wantErr)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	out, err := exec.Command(buildBinary(t), "-version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "dev" {
		t.Fatalf("-version = %q, %v", out, err)
	}
}
