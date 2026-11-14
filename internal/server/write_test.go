package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// recorder remembers the JSON bodies POSTed to a route and counts them.
type recorder struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (r *recorder) handler(status int, reply string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		r.mu.Lock()
		r.bodies = append(r.bodies, m)
		r.mu.Unlock()
		if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *recorder) last() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[len(r.bodies)-1]
}

const writeOn = `, "enable_write_tools": true`

func TestWriteToolsAreOffUnlessEnabled(t *testing.T) {
	for _, tc := range []struct {
		extra string
		want  int
	}{{"", 7}, {writeOn, 11}} {
		cs, _, _ := connectFake(t, tc.extra)
		res, err := cs.ListTools(t.Context(), nil)
		if err != nil || len(res.Tools) != tc.want {
			t.Fatalf("extra %q: %d tools, err %v; want %d", tc.extra, len(res.Tools), err, tc.want)
		}
		for _, tool := range res.Tools {
			isWrite := map[string]bool{"create_branch": true, "commit_files": true, "create_merge_request": true, "add_mr_note": true}[tool.Name]
			a := tool.Annotations
			if a == nil || a.OpenWorldHint == nil || *a.OpenWorldHint {
				t.Errorf("%s: must be closed-world", tool.Name)
				continue
			}
			if isWrite && (a.ReadOnlyHint || a.DestructiveHint == nil) {
				t.Errorf("%s: a write tool must not be read-only and must state destructiveHint (%+v)", tool.Name, a)
			}
			if !isWrite && !a.ReadOnlyHint {
				t.Errorf("%s: read tool must be read-only", tool.Name)
			}
			if tool.Name == "commit_files" && !*a.DestructiveHint {
				t.Error("commit_files may overwrite content and must be flagged destructive")
			}
		}
	}
}

func TestCreateBranch(t *testing.T) {
	cs, f, buf := connectFake(t, writeOn)
	f.reply("/api/v4/projects/7", `{"default_branch":"main"}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/repository/branches", rec.handler(201, `{"name":"agent/fix","protected":false,"default":false,"commit":{"id":"abc1234def"}}`))

	res, out := call(t, cs, "create_branch", map[string]any{"project": "team/service-a", "branch": "agent/fix"})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	if b := rec.last(); b["branch"] != "agent/fix" || b["ref"] != "main" {
		t.Errorf("request body = %v", b)
	}
	if out["branch"] != "agent/fix" || out["from"] != "main" || out["commit_id"] != "abc1234def" {
		t.Errorf("output = %v", out)
	}
	if line := buf.String(); !strings.Contains(line, `"tool":"create_branch"`) || !strings.Contains(line, `"ref":"agent/fix"`) || !strings.Contains(line, `"outcome":"ok"`) {
		t.Errorf("audit: %s", line)
	}

	res, out = call(t, cs, "create_branch", map[string]any{"project": "7", "branch": "agent/other", "from": "v1.2.0"})
	if res.IsError || out["from"] != "v1.2.0" || rec.last()["ref"] != "v1.2.0" {
		t.Errorf("explicit start ref ignored: %v %v", out, rec.last())
	}
}

func TestCreateBranchRefusals(t *testing.T) {
	cs, f, _ := connectFake(t, writeOn)
	f.reply("/api/v4/projects/7", `{"default_branch":"main"}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/repository/branches", rec.handler(201, `{}`))
	for _, args := range []map[string]any{
		{"project": "7", "branch": "main"},
		{"project": "7", "branch": "feature/x"},
		{"project": "7", "branch": "agent/"},
		{"project": "7", "branch": "Agent/x"},
		{"project": "7", "branch": "agent/../main"},
		{"project": "7", "branch": "agent/a b"},
		{"project": "7", "branch": "agent/x", "from": "../main"},
		{"project": "other/proj", "branch": "agent/x"},
	} {
		res, _ := call(t, cs, "create_branch", args)
		if !res.IsError || !strings.HasPrefix(textOf(t, res), "invalid input:") {
			t.Errorf("%v: got %q", args, textOf(t, res))
		}
	}
	if rec.count() != 0 {
		t.Errorf("a branch was requested %d times for refused input", rec.count())
	}
	f.on("/api/v4/projects/7/repository/branches", rec.handler(400, `{"message":"Branch already exists"}`))
	res, _ := call(t, cs, "create_branch", map[string]any{"project": "7", "branch": "agent/x"})
	if !res.IsError || textOf(t, res) != "GitLab returned HTTP 400: Branch already exists" {
		t.Errorf("existing branch: %q", textOf(t, res))
	}
}

func branchInfo(f *fakeGitLab, name, body string) {
	f.reply("/api/v4/projects/7/repository/branches/"+strings.ReplaceAll(name, "/", "%2F"), body)
}

func TestCommitFiles(t *testing.T) {
	cs, f, buf := connectFake(t, writeOn)
	branchInfo(f, "agent/fix", `{"name":"agent/fix","protected":false,"default":false}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/repository/commits", rec.handler(201, `{"id":"deadbeef01","short_id":"deadbee","title":"x","web_url":"http://gitlab.internal.invalid/c"}`))

	res, out := call(t, cs, "commit_files", map[string]any{
		"project": "team/service-a", "branch": "agent/fix", "message": "Fix the thing\n\nWhy it broke.",
		"files": []map[string]any{
			{"path": "src/new.go", "action": "create", "content": "package src\n"},
			{"path": "src/old.go", "action": "update", "content": "package src // v2\n", "last_commit_id": "abc1234"},
			{"path": "src/empty.txt", "action": "create"},
			{"path": "src/gone.go", "action": "delete", "last_commit_id": "abc1234def"},
		},
	})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	if out["commit_id"] != "deadbeef01" || out["branch"] != "agent/fix" || out["files_changed"] != float64(4) {
		t.Errorf("output = %v", out)
	}
	if strings.Contains(textOf(t, res), "internal.invalid") {
		t.Error("link to the private instance returned")
	}
	b := rec.last()
	if b["branch"] != "agent/fix" || b["commit_message"] != "Fix the thing\n\nWhy it broke." {
		t.Errorf("body = %v", b)
	}
	acts := b["actions"].([]any)
	if len(acts) != 4 {
		t.Fatalf("actions = %v", acts)
	}
	a1, a2, a3, a4 := acts[0].(map[string]any), acts[1].(map[string]any), acts[2].(map[string]any), acts[3].(map[string]any)
	if a1["action"] != "create" || a1["file_path"] != "src/new.go" || a1["content"] != "package src\n" || a1["last_commit_id"] != nil {
		t.Errorf("create action = %v", a1)
	}
	if a2["action"] != "update" || a2["last_commit_id"] != "abc1234" || a2["content"] != "package src // v2\n" {
		t.Errorf("update action = %v", a2)
	}
	if c, present := a3["content"]; !present || c != "" {
		t.Errorf("an empty file must still be sent with empty content: %v", a3)
	}
	if _, present := a4["content"]; present || a4["action"] != "delete" {
		t.Errorf("delete action must carry no content: %v", a4)
	}
	if line := buf.String(); !strings.Contains(line, `"tool":"commit_files"`) || !strings.Contains(line, `"ref":"agent/fix"`) || !strings.Contains(line, `"outcome":"ok"`) || strings.Contains(line, "package src") {
		t.Errorf("audit must record the call but never the content: %s", line)
	}
}

func TestCommitFilesRefusals(t *testing.T) {
	cs, f, buf := connectFake(t, writeOn+`, "limits": {"max_files_per_commit": 3, "max_file_bytes": 20, "max_commit_bytes": 30, "max_text_bytes": 100}`)
	branchInfo(f, "agent/fix", `{"name":"agent/fix","protected":false,"default":false}`)
	branchInfo(f, "agent/protected", `{"name":"agent/protected","protected":true,"default":false}`)
	branchInfo(f, "agent/isdefault", `{"name":"agent/isdefault","protected":false,"default":true}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/repository/commits", rec.handler(201, `{"id":"deadbeef01"}`))

	ok := func(path string) map[string]any {
		return map[string]any{"path": path, "action": "create", "content": "x"}
	}
	cases := []struct {
		name, want string
		args       map[string]any
	}{
		{"main branch", "invalid input:", map[string]any{"branch": "main", "message": "m", "files": []any{ok("a.txt")}}},
		{"foreign branch", "invalid input:", map[string]any{"branch": "feature/x", "message": "m", "files": []any{ok("a.txt")}}},
		{"blank message", "invalid input:", map[string]any{"branch": "agent/fix", "message": "  ", "files": []any{ok("a.txt")}}},
		{"long message", "invalid input:", map[string]any{"branch": "agent/fix", "message": strings.Repeat("m", 101), "files": []any{ok("a.txt")}}},
		{"no files", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{}}},
		{"too many files", "not allowed:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok("a"), ok("b"), ok("c"), ok("d")}}},
		{"bad action", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "rename", "content": "x"}}}},
		{"ci config", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok(".gitlab-ci.yml")}}},
		{"ci config other case", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok(".GitLab-CI.yml")}}},
		{"gitlab dir", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok(".gitlab/issue_templates/x.md")}}},
		{"codeowners", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok("CODEOWNERS")}}},
		{"traversal", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok("../etc/passwd")}}},
		{"git dir", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok(".git/hooks/pre-commit")}}},
		{"absolute", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok("/etc/passwd")}}},
		{"duplicate", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{ok("a.txt"), ok("A.TXT")}}},
		{"binary content", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "create", "content": "x\u0000y"}}}},
		{"file too big", "not allowed:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "create", "content": strings.Repeat("x", 21)}}}},
		{"commit too big", "not allowed:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{
			map[string]any{"path": "a", "action": "create", "content": strings.Repeat("x", 16)},
			map[string]any{"path": "b", "action": "create", "content": strings.Repeat("x", 16)}}}},
		{"delete with content", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "delete", "content": "x"}}}},
		{"create with last_commit_id", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "create", "content": "x", "last_commit_id": "abc1234"}}}},
		{"bad last_commit_id", "invalid input:", map[string]any{"branch": "agent/fix", "message": "m", "files": []any{map[string]any{"path": "a", "action": "update", "content": "x", "last_commit_id": "../x"}}}},
		{"protected branch", "not allowed:", map[string]any{"branch": "agent/protected", "message": "m", "files": []any{ok("a.txt")}}},
		{"default branch", "not allowed:", map[string]any{"branch": "agent/isdefault", "message": "m", "files": []any{ok("a.txt")}}},
		{"unknown project", "invalid input:", map[string]any{"project": "other/proj", "branch": "agent/fix", "message": "m", "files": []any{ok("a.txt")}}},
	}
	for _, c := range cases {
		args := c.args
		if _, has := args["project"]; !has {
			args["project"] = "7"
		}
		res, _ := call(t, cs, "commit_files", args)
		if !res.IsError || !strings.HasPrefix(textOf(t, res), c.want) {
			t.Errorf("%s: got %q, want an error starting with %q", c.name, textOf(t, res), c.want)
		}
	}
	if rec.count() != 0 {
		t.Errorf("a commit was sent %d times for refused input", rec.count())
	}
	if n := strings.Count(buf.String(), `"outcome":"denied"`); n != len(cases) {
		t.Errorf("want %d denied audit lines, got %d", len(cases), n)
	}

	// A missing required field is stopped by the SDK's schema validation before the handler runs:
	// still an error and still no request, but (known gap) not an audit line.
	res, _ := call(t, cs, "commit_files", map[string]any{"project": "7", "branch": "agent/fix", "message": "m",
		"files": []any{map[string]any{"path": "a", "content": "x"}}})
	if !res.IsError || !strings.Contains(textOf(t, res), "missing properties") || rec.count() != 0 {
		t.Errorf("missing action: %q (commits sent: %d)", textOf(t, res), rec.count())
	}

	// A branch that does not exist is reported, and nothing is committed.
	res, _ = call(t, cs, "commit_files", map[string]any{"project": "7", "branch": "agent/missing", "message": "m", "files": []any{ok("a.txt")}})
	if !res.IsError || !strings.HasPrefix(textOf(t, res), "GitLab returned HTTP 404") || rec.count() != 0 {
		t.Errorf("missing branch: %q (commits sent: %d)", textOf(t, res), rec.count())
	}
}

func TestCommitFilesGitLabErrorAndDisabledDenyList(t *testing.T) {
	cs, f, _ := connectFake(t, writeOn+`, "write_deny_paths": []`)
	branchInfo(f, "agent/fix", `{"name":"agent/fix","protected":false,"default":false}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/repository/commits", rec.handler(400, `{"message":"A file with this name doesn't exist"}`))
	res, _ := call(t, cs, "commit_files", map[string]any{"project": "7", "branch": "agent/fix", "message": "m",
		"files": []any{map[string]any{"path": ".gitlab-ci.yml", "action": "update", "content": "x"}}})
	if rec.count() != 1 {
		t.Fatal("with an explicitly empty deny-list the commit must reach GitLab")
	}
	if !res.IsError || textOf(t, res) != "GitLab returned HTTP 400: A file with this name doesn't exist" {
		t.Errorf("got %q", textOf(t, res))
	}
}

func TestDraftTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Fix bug":         "Draft: Fix bug",
		"  Fix bug  ":     "Draft: Fix bug",
		"Draft: Fix bug":  "Draft: Fix bug",
		"draft: Fix bug":  "draft: Fix bug",
		"[Draft] Fix bug": "[Draft] Fix bug",
		"(DRAFT) Fix bug": "(DRAFT) Fix bug",
		"Drafting docs":   "Draft: Drafting docs",
		"Redraft: x":      "Draft: Redraft: x",
	} {
		if got := draftTitle(in); got != want {
			t.Errorf("draftTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateMergeRequest(t *testing.T) {
	cs, f, _ := connectFake(t, writeOn)
	f.reply("/api/v4/projects/7", `{"default_branch":"main"}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/merge_requests", rec.handler(201, `{"iid":12,"title":"Draft: Fix the thing","state":"opened","source_branch":"agent/fix","target_branch":"main","web_url":"http://gitlab.internal.invalid/mr"}`))

	res, out := call(t, cs, "create_merge_request", map[string]any{"project": "team/service-a", "source_branch": "agent/fix", "title": "Fix the thing", "description": "Line one\n\nLine two"})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	b := rec.last()
	if b["source_branch"] != "agent/fix" || b["target_branch"] != "main" || b["title"] != "Draft: Fix the thing" || b["description"] != "Line one\n\nLine two" {
		t.Errorf("body = %v", b)
	}
	if out["iid"] != float64(12) || out["draft"] != true || out["state"] != "opened" || out["target_branch"] != "main" {
		t.Errorf("output = %v", out)
	}
	if strings.Contains(textOf(t, res), "internal.invalid") {
		t.Error("link to the private instance returned")
	}
}

func TestCreateMergeRequestRefusals(t *testing.T) {
	cs, f, _ := connectFake(t, writeOn)
	f.reply("/api/v4/projects/7", `{"default_branch":"agent/trunk"}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/merge_requests", rec.handler(201, `{}`))
	for _, c := range []struct {
		name, want string
		args       map[string]any
	}{
		{"default as source", "invalid input:", map[string]any{"source_branch": "main", "title": "t"}},
		{"foreign source", "invalid input:", map[string]any{"source_branch": "feature/x", "title": "t"}},
		{"blank title", "invalid input:", map[string]any{"source_branch": "agent/x", "title": "  "}},
		{"multi-line title", "invalid input:", map[string]any{"source_branch": "agent/x", "title": "a\nb"}},
		{"title too long with prefix", "invalid input:", map[string]any{"source_branch": "agent/x", "title": strings.Repeat("t", 255)}},
		{"control chars in description", "invalid input:", map[string]any{"source_branch": "agent/x", "title": "t", "description": "a\u202eb"}},
		{"source is the default branch", "not allowed:", map[string]any{"source_branch": "agent/trunk", "title": "t"}},
	} {
		c.args["project"] = "7"
		res, _ := call(t, cs, "create_merge_request", c.args)
		if !res.IsError || !strings.HasPrefix(textOf(t, res), c.want) {
			t.Errorf("%s: got %q", c.name, textOf(t, res))
		}
	}
	if rec.count() != 0 {
		t.Errorf("a merge request was created %d times for refused input", rec.count())
	}
}

func TestAddMRNote(t *testing.T) {
	cs, f, _ := connectFake(t, writeOn)
	f.reply("/api/v4/projects/7/merge_requests/12", `{"iid":12,"source_branch":"agent/fix","state":"opened"}`)
	f.reply("/api/v4/projects/7/merge_requests/13", `{"iid":13,"source_branch":"feature/human","state":"opened"}`)
	f.reply("/api/v4/projects/7/merge_requests/14", `{"iid":14,"source_branch":"agent/old","state":"merged"}`)
	rec := &recorder{}
	f.on("/api/v4/projects/7/merge_requests/12/notes", rec.handler(201, `{"id":991,"body":"x"}`))
	f.on("/api/v4/projects/7/merge_requests/13/notes", rec.handler(201, `{"id":1}`))
	f.on("/api/v4/projects/7/merge_requests/14/notes", rec.handler(201, `{"id":2}`))

	res, out := call(t, cs, "add_mr_note", map[string]any{"project": "7", "merge_request_iid": 12, "body": "Addressed the review.\n\nPlease look again."})
	if res.IsError || out["note_id"] != float64(991) || out["merge_request_iid"] != float64(12) || rec.last()["body"] != "Addressed the review.\n\nPlease look again." {
		t.Fatalf("happy path: %v %q", out, textOf(t, res))
	}
	sent := rec.count()

	for _, c := range []struct {
		iid  int
		body string
		want string
	}{
		{13, "hi", "not allowed: comments are only allowed on merge requests from branches starting with agent/"},
		{14, "hi", "not allowed: the merge request is merged, not open"},
		{12, "  ", "invalid input:"},
		{0, "hi", "invalid input:"},
		{404, "hi", "GitLab returned HTTP 404"},
	} {
		res, _ := call(t, cs, "add_mr_note", map[string]any{"project": "7", "merge_request_iid": c.iid, "body": c.body})
		if !res.IsError || !strings.HasPrefix(textOf(t, res), c.want) {
			t.Errorf("iid %d: got %q, want prefix %q", c.iid, textOf(t, res), c.want)
		}
	}
	if rec.count() != sent {
		t.Errorf("a note was posted for a refused call (%d -> %d)", sent, rec.count())
	}
}
