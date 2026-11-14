package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

type bytesBuffer = bytes.Buffer

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func fileJSON(content []byte) string {
	return fmt.Sprintf(`{"file_name":"x","size":%d,"encoding":"base64","content":%q,"blob_id":"blob1","last_commit_id":"commit1"}`, len(content), b64(content))
}

func TestGetTree(t *testing.T) {
	cs, f, buf := connectFake(t, "")
	var q url.Values
	f.on("/api/v4/projects/7/repository/tree", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Header().Set("X-Next-Page", "2")
		io.WriteString(w, `[{"path":"src","type":"tree"},{"path":"README.md","type":"blob"},{"path":"vendor/lib","type":"commit"}]`)
	})
	res, out := call(t, cs, "get_tree", map[string]any{"project": "team/service-a", "path": "src", "ref": "agent/x", "recursive": true, "page": 1})
	if res.IsError {
		t.Fatalf("error: %s", textOf(t, res))
	}
	for k, want := range map[string]string{"per_page": "100", "page": "1", "path": "src", "ref": "agent/x", "recursive": "true"} {
		if q.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}
	entries, _ := out["entries"].([]any)
	var types []string
	for _, e := range entries {
		types = append(types, e.(map[string]any)["type"].(string))
	}
	if strings.Join(types, ",") != "dir,file,submodule" || out["next_page"] != float64(2) || out["page"] != float64(1) {
		t.Errorf("unexpected output: %v", out)
	}
	if line := buf.String(); !strings.Contains(line, `"tool":"get_tree"`) || !strings.Contains(line, `"project_id":7`) || !strings.Contains(line, `"ref":"agent/x"`) {
		t.Errorf("audit: %s", line)
	}
}

func TestGetTreeDefaultsAndLastPage(t *testing.T) {
	cs, f, _ := connectFake(t, "")
	var q url.Values
	f.on("/api/v4/projects/12/repository/tree", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		io.WriteString(w, `[]`)
	})
	res, out := call(t, cs, "get_tree", map[string]any{"project": "12"})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	if _, has := q["ref"]; has || q.Get("page") != "1" || q.Has("path") || q.Has("recursive") {
		t.Errorf("unexpected query: %v", q)
	}
	if _, has := out["next_page"]; has {
		t.Errorf("next_page must be absent on the last page: %v", out)
	}
	if entries, ok := out["entries"].([]any); !ok || len(entries) != 0 {
		t.Errorf("entries must be an empty list: %v", out["entries"])
	}
}

func TestInputValidationMakesNoRequest(t *testing.T) {
	cs, f, buf := connectFake(t, "")
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"get_tree", map[string]any{"project": "other/proj"}},
		{"get_tree", map[string]any{"project": "99"}},
		{"get_file", map[string]any{"project": "other/proj", "path": "a.txt"}},
		{"list_pipelines", map[string]any{"project": "other/proj"}},
		{"list_pipeline_jobs", map[string]any{"project": "other/proj", "pipeline_id": 1}},
		{"get_job_trace", map[string]any{"project": "other/proj", "job_id": 1}},
		{"get_mr_diff", map[string]any{"project": "other/proj", "merge_request_iid": 1}},
		{"get_mr_diff", map[string]any{"project": "../../x", "merge_request_iid": 1}},
		{"get_tree", map[string]any{"project": "7", "path": "../etc"}},
		{"get_tree", map[string]any{"project": "7", "ref": "a b"}},
		{"get_tree", map[string]any{"project": "7", "page": -1}},
		{"get_tree", map[string]any{"project": "7", "page": 10001}},
		{"get_file", map[string]any{"project": "7", "path": ""}},
		{"get_file", map[string]any{"project": "7", "path": "/etc/passwd"}},
		{"get_file", map[string]any{"project": "7", "path": "a/.git/config"}},
		{"get_file", map[string]any{"project": "7", "path": "x", "ref": "../main"}},
		{"list_pipelines", map[string]any{"project": "7", "status": "bogus"}},
		{"list_pipelines", map[string]any{"project": "7", "ref": "x y"}},
		{"list_pipeline_jobs", map[string]any{"project": "7", "pipeline_id": 0}},
		{"list_pipeline_jobs", map[string]any{"project": "7", "pipeline_id": -5}},
		{"get_job_trace", map[string]any{"project": "7", "job_id": 0}},
		{"get_mr_diff", map[string]any{"project": "7", "merge_request_iid": 0}},
		{"get_mr_diff", map[string]any{"project": "7", "merge_request_iid": 3, "page": -2}},
	}
	for _, c := range cases {
		res, _ := call(t, cs, c.tool, c.args)
		if !res.IsError || !strings.HasPrefix(textOf(t, res), "invalid input:") {
			t.Errorf("%s %v: want an invalid-input error, got %q", c.tool, c.args, textOf(t, res))
		}
	}
	if f.count() != 0 {
		t.Errorf("GitLab was contacted %d times for invalid input: %v", f.count(), f.uris())
	}
	if n := strings.Count(buf.String(), `"outcome":"denied"`); n != len(cases) {
		t.Errorf("want %d denied audit lines, got %d", len(cases), n)
	}
}

func TestGetFileTextWithEscapedPathAndCachedDefaultBranch(t *testing.T) {
	cs, f, _ := connectFake(t, "")
	f.reply("/api/v4/projects/7", `{"id":7,"default_branch":"main"}`)
	var q url.Values
	f.on("/api/v4/projects/7/repository/files/src%2Fmain.go", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		io.WriteString(w, fileJSON([]byte("package main\n")))
	})
	for i := 0; i < 2; i++ {
		res, out := call(t, cs, "get_file", map[string]any{"project": "team/service-a", "path": "src/main.go"})
		if res.IsError {
			t.Fatal(textOf(t, res))
		}
		if out["content"] != "package main\n" || out["ref"] != "main" || out["binary"] != false || out["size"] != float64(13) ||
			out["blob_id"] != "blob1" || out["last_commit_id"] != "commit1" || out["path"] != "src/main.go" {
			t.Errorf("unexpected output: %v", out)
		}
	}
	if q.Get("ref") != "main" {
		t.Errorf("file request ref = %q", q.Get("ref"))
	}
	if n := f.countPath("/api/v4/projects/7"); n != 1 {
		t.Errorf("default branch looked up %d times, want 1 (cached)", n)
	}
	res, out := call(t, cs, "get_file", map[string]any{"project": "7", "path": "src/main.go", "ref": "agent/x"})
	if res.IsError || out["ref"] != "agent/x" || q.Get("ref") != "agent/x" {
		t.Errorf("explicit ref ignored: %v %v", out, q)
	}
}

func TestGetFileBinaryIsNotReturned(t *testing.T) {
	cs, f, _ := connectFake(t, "")
	f.reply("/api/v4/projects/7/repository/files/logo.png", fileJSON([]byte{0x89, 'P', 'N', 'G', 0x00, 0x01}))
	f.reply("/api/v4/projects/7/repository/files/latin1.txt", fileJSON([]byte{'c', 'a', 'f', 0xe9}))
	f.reply("/api/v4/projects/7/repository/files/nul.txt", fileJSON([]byte("valid utf-8 but\x00has a NUL byte")))
	for _, path := range []string{"logo.png", "latin1.txt", "nul.txt"} {
		res, out := call(t, cs, "get_file", map[string]any{"project": "7", "path": path, "ref": "main"})
		if res.IsError || out["binary"] != true {
			t.Fatalf("%s: %v %s", path, out, textOf(t, res))
		}
		if _, has := out["content"]; has {
			t.Errorf("%s: binary content returned: %v", path, out)
		}
	}
}

func TestGetFileTooLarge(t *testing.T) {
	cs, f, buf := connectFake(t, `, "limits": {"max_file_bytes": 10}`)
	f.reply("/api/v4/projects/7/repository/files/big.txt", fileJSON([]byte(strings.Repeat("x", 20))))
	res, _ := call(t, cs, "get_file", map[string]any{"project": "7", "path": "big.txt", "ref": "main"})
	if !res.IsError || textOf(t, res) != "not allowed: the file is 20 bytes, which exceeds the limit of 10 bytes" {
		t.Errorf("got %q", textOf(t, res))
	}
	if !strings.Contains(buf.String(), `"outcome":"denied"`) {
		t.Errorf("audit: %s", buf)
	}
}

func TestGitLabFailuresAreReportedSafely(t *testing.T) {
	cs, f, buf := connectFake(t, "")
	f.on("/api/v4/projects/7/repository/files/missing.txt", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"message":"404 File Not Found"}`)
	})
	f.on("/api/v4/projects/7/repository/files/down.txt", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		io.WriteString(w, "<html>bad gateway from 10.9.8.7</html>")
	})
	f.on("/api/v4/projects/7/repository/files/redirect.txt", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.invalid/", http.StatusFound)
	})
	f.reply("/api/v4/projects/7/repository/files/weird.txt", `{"encoding":"text","content":"x"}`)
	want := map[string]string{
		"missing.txt":  "GitLab returned HTTP 404: 404 File Not Found",
		"down.txt":     "GitLab returned HTTP 502",
		"redirect.txt": "GitLab answered with a redirect, which this server refuses to follow",
		"weird.txt":    "internal error",
	}
	for path, text := range want {
		res, _ := call(t, cs, "get_file", map[string]any{"project": "7", "path": path, "ref": "main"})
		if !res.IsError || textOf(t, res) != text {
			t.Errorf("%s: model sees %q, want %q", path, textOf(t, res), text)
		}
	}
	if strings.Contains(buf.String(), tok) || strings.Contains(buf.String(), "10.9.8.7") {
		t.Errorf("audit log leaks details: %s", buf)
	}
}

func TestGetMRDiffRespectsSizeBudget(t *testing.T) {
	cs, f, _ := connectFake(t, `, "limits": {"max_diff_bytes": 50}`)
	var q url.Values
	f.on("/api/v4/projects/7/merge_requests/12/diffs", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		fmt.Fprintf(w, `[
		  {"old_path":"a.go","new_path":"a.go","diff":%q},
		  {"old_path":"b.go","new_path":"b.go","new_file":false,"diff":%q},
		  {"old_path":"c.go","new_path":"c2.go","renamed_file":true,"diff":%q},
		  {"old_path":"d.bin","new_path":"d.bin","diff":"","too_large":true}
		]`, strings.Repeat("a", 31), strings.Repeat("\u00e9", 20), strings.Repeat("c", 10))
	})
	res, out := call(t, cs, "get_mr_diff", map[string]any{"project": "7", "merge_request_iid": 12, "page": 2})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	if q.Get("per_page") != "20" || q.Get("page") != "2" {
		t.Errorf("query: %v", q)
	}
	files := out["files"].([]any)
	if len(files) != 4 || out["truncated"] != true {
		t.Fatalf("unexpected output: %v", out)
	}
	total := 0
	for i, raw := range files {
		fm := raw.(map[string]any)
		d, _ := fm["diff"].(string)
		total += len(d)
		if !utf8.ValidString(d) {
			t.Errorf("file %d: diff cut inside a character", i)
		}
	}
	if total > 50 {
		t.Errorf("total diff bytes = %d, want at most 50", total)
	}
	f0, f1, f2, f3 := files[0].(map[string]any), files[1].(map[string]any), files[2].(map[string]any), files[3].(map[string]any)
	if f0["truncated"] != nil || len(f0["diff"].(string)) != 31 {
		t.Errorf("first file must be intact: %v", f0)
	}
	if f1["truncated"] != true || len(f1["diff"].(string)) != 18 {
		t.Errorf("second file must be cut at a character boundary: %v", f1)
	}
	if f2["truncated"] != true || f2["diff"] != "c" || f2["renamed_file"] != true || f2["new_path"] != "c2.go" {
		t.Errorf("third file must keep its names and get only the remaining byte: %v", f2)
	}
	if f3["omitted"] != true {
		t.Errorf("too_large file must be marked omitted: %v", f3)
	}
}

func TestListPipelines(t *testing.T) {
	cs, f, _ := connectFake(t, "")
	var q url.Values
	f.on("/api/v4/projects/7/pipelines", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Header().Set("X-Next-Page", "3")
		io.WriteString(w, `[{"id":501,"iid":9,"status":"failed","source":"push","ref":"agent/x","sha":"abc123","web_url":"http://gitlab.internal.invalid/x/-/pipelines/501","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:05:00Z"}]`)
	})
	res, out := call(t, cs, "list_pipelines", map[string]any{"project": "7", "ref": "agent/x", "status": "failed", "page": 2})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	for k, want := range map[string]string{"ref": "agent/x", "status": "failed", "per_page": "20", "page": "2", "order_by": "id", "sort": "desc"} {
		if q.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}
	p := out["pipelines"].([]any)[0].(map[string]any)
	if p["id"] != float64(501) || p["status"] != "failed" || p["sha"] != "abc123" || p["source"] != "push" || out["next_page"] != float64(3) || out["page"] != float64(2) {
		t.Errorf("unexpected output: %v", out)
	}
	if raw := textOf(t, res); strings.Contains(raw, "web_url") || strings.Contains(raw, "internal.invalid") {
		t.Errorf("links to the private instance must not be returned: %s", raw)
	}
	f.reply("/api/v4/projects/12/pipelines", `[]`)
	if _, out := call(t, cs, "list_pipelines", map[string]any{"project": "12"}); out["pipelines"] == nil {
		t.Errorf("empty result must be an empty list, not null: %v", out)
	}
}

func TestListPipelineJobs(t *testing.T) {
	cs, f, _ := connectFake(t, "")
	f.reply("/api/v4/projects/7/pipelines/99/jobs", `[
	  {"id":1,"name":"build","stage":"build","status":"success","allow_failure":false,"duration":12.5},
	  {"id":2,"name":"test","stage":"test","status":"failed","allow_failure":true,"failure_reason":"script_failure","duration":null}]`)
	res, out := call(t, cs, "list_pipeline_jobs", map[string]any{"project": "7", "pipeline_id": 99})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	jobs := out["jobs"].([]any)
	j1, j2 := jobs[0].(map[string]any), jobs[1].(map[string]any)
	if len(jobs) != 2 || j1["duration_seconds"] != 12.5 || j2["duration_seconds"] != nil || j2["failure_reason"] != "script_failure" || j2["allow_failure"] != true {
		t.Errorf("unexpected jobs: %v", jobs)
	}
}

func TestGetJobTraceCleansAndTruncates(t *testing.T) {
	cs, f, _ := connectFake(t, `, "limits": {"max_trace_bytes": 200}`)
	var raw strings.Builder
	raw.WriteString("section_start:1700000000:build_script\r\x1b[0K\x1b[36;1mBuilding\x1b[0;m\r\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&raw, "line %d \x1b[32;1mok\x1b[0m\r\n", i)
	}
	raw.WriteString("section_end:1700000001:build_script\r\x1b[0K\r\n\x1b]0;title\x07done\n")
	f.on("/api/v4/projects/7/jobs/55/trace", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, raw.String()) })

	res, out := call(t, cs, "get_job_trace", map[string]any{"project": "7", "job_id": 55})
	if res.IsError {
		t.Fatal(textOf(t, res))
	}
	log := out["log"].(string)
	if out["truncated"] != true || out["total_bytes"] != float64(raw.Len()) || out["job_id"] != float64(55) {
		t.Errorf("metadata: truncated=%v total=%v", out["truncated"], out["total_bytes"])
	}
	if len(log) > 200 || !utf8.ValidString(log) {
		t.Errorf("log is %d bytes or invalid UTF-8", len(log))
	}
	for _, bad := range []string{"\x1b", "\r", "section_", "\x07"} {
		if strings.Contains(log, bad) {
			t.Errorf("log still contains %q: %q", bad, log)
		}
	}
	if !strings.HasPrefix(log, "line ") || !strings.HasSuffix(log, "line 99 ok\n\ndone\n") {
		t.Errorf("log must start at a line boundary and end with the final lines: %q", log)
	}

	f.on("/api/v4/projects/7/jobs/56/trace", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "short \x1b[31mlog\x1b[0m\n") })
	_, out = call(t, cs, "get_job_trace", map[string]any{"project": "7", "job_id": 56})
	if out["truncated"] != false || out["log"] != "short log\n" {
		t.Errorf("short log: %v", out)
	}
}

func TestCleanLog(t *testing.T) {
	cases := []struct {
		in        string
		truncated bool
		want      string
	}{
		{"plain\n", false, "plain\n"},
		{"\x1b[31;1mred\x1b[0m text\r\n", false, "red text\n"},
		{"progress 10%\rprogress 50%\rdone\n", false, "progress 10%\nprogress 50%\ndone\n"},
		{"partial first line\nfull\n", true, "full\n"},
		{"partial first line\nfull\n", false, "partial first line\nfull\n"},
		{"a\x00b\x1bc\x7fd\u202ee\n", false, "abcde\n"},
		{"tab\tkept\n", false, "tab\tkept\n"},
		{"\x1b]0;window title\x07x\n", false, "x\n"},
		{"\x1b]8;;http://example.invalid\x1b\\link\x1b]8;;\x1b\\\n", false, "link\n"},
		{"\x1b(Bcharset\n", false, "charset\n"},
		{"one\n\n\n\n\ntwo\n", false, "one\n\ntwo\n"},
		{"bad \xff byte\n", false, "bad \uFFFD byte\n"},
		{"section_start:1700000000:step_script\r\x1b[0Kstep\n", false, "\nstep\n"},
	}
	for _, c := range cases {
		if got := cleanLog([]byte(c.in), c.truncated); got != c.want {
			t.Errorf("cleanLog(%q, %v) = %q, want %q", c.in, c.truncated, got, c.want)
		}
	}
}

func TestCutUTF8(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"}, {"abc", 2, "ab"}, {"abc", 0, ""}, {"abc", -1, ""}, {"\u00e9\u00e9", 3, "\u00e9"}, {"\u00e9", 1, ""},
	} {
		if got := cutUTF8(c.in, c.n); got != c.want {
			t.Errorf("cutUTF8(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
