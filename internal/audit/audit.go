// Package audit writes one structured JSON line per tool call. It never
// writes to stdout, because stdout is the MCP protocol channel.
package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
)

const (
	redacted     = "[REDACTED]"
	minSecretLen = 8
	maxField     = 200
	maxReason    = 500
)

// Outcome classifies a tool call.
type Outcome string

// Possible outcomes.
const (
	OK     Outcome = "ok"
	Denied Outcome = "denied" // rejected by policy or validation before any request
	Failed Outcome = "error"  // allowed, but failed (GitLab error, timeout, ...)
)

// Event describes one tool call. It deliberately has no field for file
// contents, request bodies or headers.
type Event struct {
	Tool      string
	ProjectID int64
	Ref       string // branch or ref the call targeted, if any
	Outcome   Outcome
	Reason    string // short, human-readable reason for Denied/Failed
	Duration  time.Duration
}

// Logger writes audit events.
type Logger struct {
	log     *slog.Logger
	secrets []string
}

// New returns a Logger writing JSON lines to w. Any occurrence of the given
// secrets in logged text is replaced. It refuses os.Stdout.
func New(w io.Writer, secrets ...secret.Secret) (*Logger, error) {
	if w == nil {
		return nil, errors.New("audit: nil writer")
	}
	if f, ok := w.(*os.File); ok && (f == os.Stdout || f.Name() == "/dev/stdout") {
		return nil, errors.New("audit: refusing to log to stdout (reserved for the MCP protocol)")
	}
	l := &Logger{log: slog.New(slog.NewJSONHandler(w, nil))}
	for _, s := range secrets {
		if v := s.Reveal(); len(v) >= minSecretLen {
			l.secrets = append(l.secrets, v)
		}
	}
	return l, nil
}

// NewStderr returns a Logger writing to the process's stderr.
func NewStderr(secrets ...secret.Secret) *Logger {
	l, err := New(os.Stderr, secrets...)
	if err != nil { // unreachable unless stderr is redirected to /dev/stdout by name
		panic(err)
	}
	return l
}

// Log records e.
func (l *Logger) Log(ctx context.Context, e Event) {
	level := slog.LevelInfo
	if e.Outcome != OK {
		level = slog.LevelWarn
	}
	l.log.LogAttrs(ctx, level, "tool_call",
		slog.String("tool", l.clean(e.Tool, maxField)),
		slog.Int64("project_id", e.ProjectID),
		slog.String("ref", l.clean(e.Ref, maxField)),
		slog.String("outcome", string(e.Outcome)),
		slog.String("reason", l.clean(e.Reason, maxReason)),
		slog.Int64("duration_ms", e.Duration.Milliseconds()),
	)
}

func (l *Logger) clean(s string, max int) string {
	for _, v := range l.secrets {
		s = strings.ReplaceAll(s, v, redacted)
	}
	if len(s) > max {
		s = s[:max]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
		s += "..."
	}
	return s
}
