package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeGitLab is a stand-in GitLab API. Routes are keyed by escaped URL path.
type fakeGitLab struct {
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	hits   []string
}

func newFake(t *testing.T) *fakeGitLab {
	t.Helper()
	f := &fakeGitLab{routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.RequestURI)
		h := f.routes[r.URL.EscapedPath()]
		f.mu.Unlock()
		switch {
		case r.Header.Get("PRIVATE-TOKEN") != tok:
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"401 Unauthorized"}`)
		case h == nil:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"404 Not Found"}`)
		default:
			h(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitLab) on(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = h
}

// reply registers a JSON response; headers are name/value pairs.
func (f *fakeGitLab) reply(path, body string, headers ...string) {
	f.on(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		io.WriteString(w, body)
	})
}

func (f *fakeGitLab) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

func (f *fakeGitLab) countPath(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, h := range f.hits {
		if u := h; len(u) >= len(path) && u[:len(path)] == path && (len(u) == len(path) || u[len(path)] == '?') {
			n++
		}
	}
	return n
}

func (f *fakeGitLab) uris() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

// call invokes a tool and returns the raw result plus the decoded structured output.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var out map[string]any
	if !res.IsError && res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return res, out
}

// connectFake returns a client session for a server backed by a fake GitLab.
func connectFake(t *testing.T, extraConfig string) (*mcp.ClientSession, *fakeGitLab, *bytesBuffer) {
	t.Helper()
	f := newFake(t)
	deps, buf := testDepsFor(t, f.srv.URL, extraConfig)
	srv, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	return connectTo(t, srv), f, (*bytesBuffer)(buf)
}
