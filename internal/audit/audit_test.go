package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
)

const tok = "glpat-supersecret-123"

func newLogger(t *testing.T) (*Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	l, err := New(&buf, secret.New(tok))
	if err != nil {
		t.Fatal(err)
	}
	return l, &buf
}

func TestLogShape(t *testing.T) {
	l, buf := newLogger(t)
	l.Log(context.Background(), Event{Tool: "commit_files", ProjectID: 7, Ref: "agent/x", Outcome: OK, Duration: 42 * time.Millisecond})
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON object: %v: %s", err, buf)
	}
	want := map[string]any{"msg": "tool_call", "level": "INFO", "tool": "commit_files", "project_id": float64(7),
		"ref": "agent/x", "outcome": "ok", "duration_ms": float64(42)}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if strings.Count(buf.String(), "\n") != 1 {
		t.Errorf("want exactly one line: %q", buf)
	}
}

func TestDeniedIsWarning(t *testing.T) {
	l, buf := newLogger(t)
	l.Log(context.Background(), Event{Tool: "create_branch", Outcome: Denied, Reason: "branch must start with agent/"})
	if !strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Errorf("denied event must be a warning: %s", buf)
	}
}

func TestSecretsRedactedAndFieldsBounded(t *testing.T) {
	l, buf := newLogger(t)
	l.Log(context.Background(), Event{
		Tool: "get_file", Ref: "x" + tok, Outcome: Failed,
		Reason: "request failed: token " + tok + " rejected " + strings.Repeat("é", 1000) + "\n{\"forged\":true}",
	})
	out := buf.String()
	if strings.Contains(out, tok) {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, redacted) {
		t.Errorf("expected redaction marker: %s", out)
	}
	if len(out) > 1500 {
		t.Errorf("log line not bounded: %d bytes", len(out))
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("newline in input must not create extra log lines: %q", out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Errorf("line is not valid JSON: %v", err)
	}
	if _, forged := m["forged"]; forged {
		t.Error("input injected a JSON field")
	}
}

func TestRefusesStdout(t *testing.T) {
	if _, err := New(os.Stdout); err == nil {
		t.Fatal("New(os.Stdout) must fail")
	}
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) must fail")
	}
}

func TestNewStderrNeverTouchesStdout(t *testing.T) {
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW

	NewStderr(secret.New(tok)).Log(context.Background(), Event{Tool: "list_projects", Outcome: OK})

	outW.Close()
	errW.Close()
	gotOut, _ := io.ReadAll(outR)
	gotErr, _ := io.ReadAll(errR)
	if len(gotOut) != 0 {
		t.Errorf("stdout was written to: %q", gotOut)
	}
	if !strings.Contains(string(gotErr), "list_projects") {
		t.Errorf("stderr missing event: %q", gotErr)
	}
}

func TestShortSecretsAreNotUsedForReplacement(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New(&buf, secret.New("abc"))
	l.Log(context.Background(), Event{Tool: "abc", Outcome: OK})
	if !strings.Contains(buf.String(), `"tool":"abc"`) {
		t.Errorf("short secret must not mangle unrelated text: %s", buf.String())
	}
}
