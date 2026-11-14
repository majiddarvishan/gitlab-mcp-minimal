// Package config loads and validates the server configuration. The
// configuration never contains the GitLab token itself, only where to read it.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

const (
	// DefaultBranchPrefix is the branch prefix the agent may write to.
	DefaultBranchPrefix = "agent/"
	// DefaultRequestTimeoutSeconds is the per-request GitLab timeout.
	DefaultRequestTimeoutSeconds = 30

	maxConfigBytes = 64 << 10
	maxTokenBytes  = 512
	maxTokenFile   = 4 << 10
	maxTimeoutSecs = 120
)

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Limits bound the size of what the server reads and writes.
type Limits struct {
	MaxFilesPerCommit int `json:"max_files_per_commit"`
	MaxFileBytes      int `json:"max_file_bytes"`
	MaxCommitBytes    int `json:"max_commit_bytes"`
	MaxResponseBytes  int `json:"max_response_bytes"`
	MaxTraceBytes     int `json:"max_trace_bytes"`
	MaxDiffBytes      int `json:"max_diff_bytes"`
	MaxTextBytes      int `json:"max_text_bytes"`
}

var (
	defaultLimits = Limits{
		MaxFilesPerCommit: 20,
		MaxFileBytes:      256 << 10,
		MaxCommitBytes:    1 << 20,
		MaxResponseBytes:  4 << 20,
		MaxTraceBytes:     64 << 10,
		MaxDiffBytes:      128 << 10,
		MaxTextBytes:      16 << 10,
	}
	// ceilingLimits are hard upper bounds a configuration cannot exceed.
	ceilingLimits = Limits{
		MaxFilesPerCommit: 100,
		MaxFileBytes:      1 << 20,
		MaxCommitBytes:    4 << 20,
		MaxResponseBytes:  16 << 20,
		MaxTraceBytes:     1 << 20,
		MaxDiffBytes:      1 << 20,
		MaxTextBytes:      64 << 10,
	}
)

// Config is the server configuration.
type Config struct {
	// BaseURL is the GitLab instance root, for example https://gitlab.example.com
	// (a path prefix is allowed for instances served under a relative URL root).
	BaseURL string `json:"base_url"`
	// AllowInsecureHTTP permits an http:// BaseURL. Off by default.
	AllowInsecureHTTP bool `json:"allow_insecure_http"`
	// CAFile is an optional absolute path to a PEM bundle for a private CA.
	CAFile string `json:"ca_file"`

	// Exactly one of TokenFile (absolute path, preferred) or TokenEnv is required.
	TokenFile string `json:"token_file"`
	TokenEnv  string `json:"token_env"`

	// Projects is the allow-list. Every tool call is checked against it.
	Projects []validate.Project `json:"projects"`

	// BranchPrefix restricts the branches the agent may write to.
	BranchPrefix string `json:"branch_prefix"`
	// WriteDenyPaths overrides validate.DefaultDenyPaths when present
	// (an explicit empty list disables the deny-list).
	WriteDenyPaths []string `json:"write_deny_paths"`

	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`
	Limits                Limits `json:"limits"`
}

// Load reads, parses and validates the configuration file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads, strictly decodes and validates a configuration. Unknown fields,
// trailing data and oversized input are rejected.
func Parse(r io.Reader) (*Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("config: read: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("config: file is larger than %d bytes", maxConfigBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: invalid JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("config: unexpected data after the JSON object")
	}
	if err := c.normalize(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

// RequestTimeout returns the per-request timeout.
func (c *Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

// Allowlist builds the project allow-list.
func (c *Config) Allowlist() (*validate.Allowlist, error) {
	return validate.NewAllowlist(c.Projects)
}

func (c *Config) normalize() error {
	base, err := normalizeBaseURL(c.BaseURL, c.AllowInsecureHTTP)
	if err != nil {
		return err
	}
	c.BaseURL = base

	switch {
	case c.TokenFile != "" && c.TokenEnv != "":
		return errors.New("set only one of token_file and token_env")
	case c.TokenFile == "" && c.TokenEnv == "":
		return errors.New("one of token_file or token_env is required")
	case c.TokenFile != "" && !filepath.IsAbs(c.TokenFile):
		return errors.New("token_file must be an absolute path")
	case c.TokenEnv != "" && !envNameRE.MatchString(c.TokenEnv):
		return errors.New("token_env is not a valid environment variable name")
	case c.CAFile != "" && !filepath.IsAbs(c.CAFile):
		return errors.New("ca_file must be an absolute path")
	}

	if _, err := c.Allowlist(); err != nil {
		return err
	}

	if c.BranchPrefix == "" {
		c.BranchPrefix = DefaultBranchPrefix
	}
	if err := validate.BranchPrefix(c.BranchPrefix); err != nil {
		return err
	}

	if c.WriteDenyPaths == nil {
		c.WriteDenyPaths = append([]string(nil), validate.DefaultDenyPaths...)
	}
	for _, d := range c.WriteDenyPaths {
		if err := validate.DenyPathEntry(d); err != nil {
			return err
		}
	}

	switch {
	case c.RequestTimeoutSeconds == 0:
		c.RequestTimeoutSeconds = DefaultRequestTimeoutSeconds
	case c.RequestTimeoutSeconds < 0 || c.RequestTimeoutSeconds > maxTimeoutSecs:
		return fmt.Errorf("request_timeout_seconds must be between 1 and %d", maxTimeoutSecs)
	}
	return c.Limits.normalize()
}

func normalizeBaseURL(raw string, allowHTTP bool) (string, error) {
	if raw == "" {
		return "", errors.New("base_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("base_url is not a valid URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowHTTP:
	case u.Scheme == "http":
		return "", errors.New("base_url uses http; set allow_insecure_http to permit it")
	default:
		return "", errors.New("base_url must use https")
	}
	switch {
	case u.Opaque != "" || u.Hostname() == "":
		return "", errors.New("base_url must include a host")
	case u.User != nil:
		return "", errors.New("base_url must not contain credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", errors.New("base_url must not contain a query or fragment")
	case u.RawPath != "" || strings.Contains(u.Path, "..") || strings.Contains(u.Path, "//") || strings.Contains(u.Path, "%"):
		return "", errors.New("base_url path is not valid")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func (l *Limits) normalize() error {
	fields := []struct {
		name         string
		val          *int
		def, ceiling int
	}{
		{"max_files_per_commit", &l.MaxFilesPerCommit, defaultLimits.MaxFilesPerCommit, ceilingLimits.MaxFilesPerCommit},
		{"max_file_bytes", &l.MaxFileBytes, defaultLimits.MaxFileBytes, ceilingLimits.MaxFileBytes},
		{"max_commit_bytes", &l.MaxCommitBytes, defaultLimits.MaxCommitBytes, ceilingLimits.MaxCommitBytes},
		{"max_response_bytes", &l.MaxResponseBytes, defaultLimits.MaxResponseBytes, ceilingLimits.MaxResponseBytes},
		{"max_trace_bytes", &l.MaxTraceBytes, defaultLimits.MaxTraceBytes, ceilingLimits.MaxTraceBytes},
		{"max_diff_bytes", &l.MaxDiffBytes, defaultLimits.MaxDiffBytes, ceilingLimits.MaxDiffBytes},
		{"max_text_bytes", &l.MaxTextBytes, defaultLimits.MaxTextBytes, ceilingLimits.MaxTextBytes},
	}
	for _, f := range fields {
		switch {
		case *f.val == 0:
			*f.val = f.def
		case *f.val < 0 || *f.val > f.ceiling:
			return fmt.Errorf("limits.%s must be between 1 and %d", f.name, f.ceiling)
		}
	}
	if l.MaxFileBytes > l.MaxCommitBytes {
		return errors.New("limits.max_file_bytes must not exceed limits.max_commit_bytes")
	}
	return nil
}

// LoadToken reads the GitLab token from the configured file or environment
// variable. The token is validated but never included in an error message.
func (c *Config) LoadToken(getenv func(string) string) (secret.Secret, error) {
	var raw string
	if c.TokenFile != "" {
		b, err := readSmallFile(c.TokenFile, maxTokenFile)
		if err != nil {
			return secret.Secret{}, fmt.Errorf("token_file: %w", err)
		}
		raw = string(b)
	} else {
		raw = getenv(c.TokenEnv)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return secret.Secret{}, errors.New("token is empty")
	}
	if len(raw) > maxTokenBytes {
		return secret.Secret{}, errors.New("token is too long")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] <= 0x20 || raw[i] >= 0x7f {
			return secret.Secret{}, errors.New("token must be a single line of printable ASCII without spaces")
		}
	}
	return secret.New(raw), nil
}

func readSmallFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("file is larger than %d bytes", max)
	}
	return b, nil
}
