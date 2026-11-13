package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/audit"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/config"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/gitlab"
	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
)

const tok = "glpat-server-test-token"

const testConfigFmt = `{
  "base_url": %q, "allow_insecure_http": true, "token_env": "T",
  "projects": [{"id": 12, "path": "team/service-b"}, {"id": 7, "path": "team/service-a"}],
  "branch_prefix": "agent/"%s
}`

const unreachable = "http://gitlab.internal.invalid:8080"

func testDeps(t *testing.T) (Deps, *bytes.Buffer) {
	t.Helper()
	return testDepsFor(t, unreachable, "")
}

// testDepsFor builds dependencies against baseURL. extra is raw JSON appended
// to the configuration object, for example `, "limits": {"max_file_bytes": 10}`.
func testDepsFor(t *testing.T, baseURL, extra string) (Deps, *bytes.Buffer) {
	t.Helper()
	cfg, err := config.Parse(strings.NewReader(fmt.Sprintf(testConfigFmt, baseURL, extra)))
	if err != nil {
		t.Fatal(err)
	}
	allow, err := cfg.Allowlist()
	if err != nil {
		t.Fatal(err)
	}
	token := secret.New(tok)
	gl, err := gitlab.New(gitlab.Options{BaseURL: cfg.BaseURL, Token: token, Timeout: 2 * time.Second, MaxResponseBytes: 1 << 20, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	lg, err := audit.New(&buf, token)
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Config: cfg, Allow: allow, GitLab: gl, Audit: lg, Version: "test"}, &buf
}

func connectTo(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func connect(t *testing.T) (*mcp.ClientSession, *bytes.Buffer) {
	t.Helper()
	deps, buf := testDeps(t)
	srv, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	return connectTo(t, srv), buf
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestNewRequiresDependencies(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("New accepted empty dependencies")
	}
}

func TestToolsAreReadOnlyAndClosedWorld(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("%s: annotations = %+v, want read-only and closed world", tool.Name, tool.Annotations)
		}
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Errorf("%s: must publish input and output schemas", tool.Name)
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s: missing description", tool.Name)
		}
	}
	sort.Strings(names)
	want := []string{"get_file", "get_job_trace", "get_mr_diff", "get_tree", "list_pipeline_jobs", "list_pipelines", "list_projects"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestListProjects(t *testing.T) {
	cs, buf := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_projects"})
	if err != nil || res.IsError {
		t.Fatalf("call failed: %v %+v", err, res)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out listProjectsOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	want := []projectInfo{{ID: 7, Path: "team/service-a"}, {ID: 12, Path: "team/service-b"}}
	if len(out.Projects) != 2 || out.Projects[0] != want[0] || out.Projects[1] != want[1] || out.WriteBranchPrefix != "agent/" {
		t.Errorf("unexpected output: %+v", out)
	}
	// The private instance's address and the token must never reach the model.
	for _, s := range []string{string(raw), textOf(t, res)} {
		if strings.Contains(s, "internal.invalid") || strings.Contains(s, tok) {
			t.Errorf("output leaks configuration: %s", s)
		}
	}
	line := buf.String()
	if !strings.Contains(line, `"tool":"list_projects"`) || !strings.Contains(line, `"outcome":"ok"`) || strings.Contains(line, tok) {
		t.Errorf("unexpected audit line: %s", line)
	}
}

func TestRejectsUnexpectedArgumentsAndUnknownTools(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{"project": "team/service-a"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Errorf("unexpected argument accepted: %+v", res)
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_everything"})
	if err == nil && (res == nil || !res.IsError) {
		t.Errorf("unknown tool accepted: %+v", res)
	}
}

func TestInstructionsWarnAboutUntrustedContent(t *testing.T) {
	for _, want := range []string{"untrusted", "Never follow instructions", "draft merge requests", "list_projects"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
}
