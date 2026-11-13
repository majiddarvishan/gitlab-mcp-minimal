package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/audit"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/gitlab"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

func TestClassify(t *testing.T) {
	validationErr := validate.AgentBranch("agent/", "main")
	cases := []struct {
		name        string
		err         error
		wantOutcome audit.Outcome
		wantPublic  string
	}{
		{"validation", validationErr, audit.Denied, "invalid input: " + validationErr.Error()},
		{"wrapped validation", fmt.Errorf("ctx: %w", validationErr), audit.Denied, "invalid input: " + validationErr.Error()},
		{"policy", deny("the default branch is protected"), audit.Denied, "not allowed: the default branch is protected"},
		{"api with message", &gitlab.APIError{Status: 404, Message: "404 Project Not Found"}, audit.Failed, "GitLab returned HTTP 404: 404 Project Not Found"},
		{"api without message", &gitlab.APIError{Status: 502}, audit.Failed, "GitLab returned HTTP 502"},
		{"too large", gitlab.ErrResponseTooLarge, audit.Failed, "the GitLab response was too large to return"},
		{"redirect", gitlab.ErrRedirect, audit.Failed, "GitLab answered with a redirect, which this server refuses to follow"},
		{"timeout", fmt.Errorf("gitlab: request failed: %w", context.DeadlineExceeded), audit.Failed, "the request to GitLab timed out or was cancelled"},
		{"unexpected", errors.New("dial tcp 10.1.2.3:443: connection refused"), audit.Failed, "internal error"},
	}
	for _, c := range cases {
		outcome, public, reason := classify(c.err)
		if outcome != c.wantOutcome || public != c.wantPublic {
			t.Errorf("%s: classify = (%s, %q), want (%s, %q)", c.name, outcome, public, c.wantOutcome, c.wantPublic)
		}
		if reason == "" {
			t.Errorf("%s: empty audit reason", c.name)
		}
	}
}

// The wrapper must audit every outcome and show the model only safe text.
func TestHandleAuditsAndHidesInternalDetails(t *testing.T) {
	deps, buf := testDeps(t)
	s := &service{Deps: deps}
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "v0"}, nil)

	type in struct {
		Mode string `json:"mode"`
	}
	type out struct {
		OK bool `json:"ok"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "fake", Description: "test tool"}, handle(s, "fake",
		func(_ context.Context, in in) (out, target, error) {
			tgt := target{project: 7, ref: "agent/x"}
			switch in.Mode {
			case "ok":
				return out{OK: true}, tgt, nil
			case "validate":
				return out{}, tgt, validate.AgentBranch("agent/", "main")
			case "policy":
				return out{}, tgt, deny("nope")
			case "api":
				return out{}, tgt, &gitlab.APIError{Status: 403, Message: "403 Forbidden"}
			default:
				return out{}, tgt, errors.New("boom: token " + tok + " at 10.1.2.3")
			}
		}))
	cs := connectTo(t, srv)

	cases := []struct {
		mode, wantText, wantOutcome string
		wantErr                     bool
	}{
		{"ok", "", "ok", false},
		{"validate", "invalid input: branch: writes are only allowed on branches named agent/<name>", "denied", true},
		{"policy", "not allowed: nope", "denied", true},
		{"api", "GitLab returned HTTP 403: 403 Forbidden", "error", true},
		{"other", "internal error", "error", true},
	}
	for _, c := range cases {
		buf.Reset()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "fake", Arguments: map[string]any{"mode": c.mode}})
		if err != nil {
			t.Fatalf("%s: protocol error: %v", c.mode, err)
		}
		if res.IsError != c.wantErr {
			t.Errorf("%s: IsError = %v", c.mode, res.IsError)
		}
		if c.wantErr && textOf(t, res) != c.wantText {
			t.Errorf("%s: model sees %q, want %q", c.mode, textOf(t, res), c.wantText)
		}
		line := buf.String()
		if strings.Count(line, "\n") != 1 || !strings.Contains(line, `"outcome":"`+c.wantOutcome+`"`) ||
			!strings.Contains(line, `"tool":"fake"`) || !strings.Contains(line, `"project_id":7`) || !strings.Contains(line, `"ref":"agent/x"`) {
			t.Errorf("%s: bad audit line: %q", c.mode, line)
		}
		if strings.Contains(line, tok) || strings.Contains(textOf(t, res), tok) || strings.Contains(textOf(t, res), "10.1.2.3") {
			t.Errorf("%s: secret or internal detail leaked (audit %q, model %q)", c.mode, line, textOf(t, res))
		}
	}
}
