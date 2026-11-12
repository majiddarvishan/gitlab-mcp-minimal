package gitlab

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
)

const tok = "glpat-test-token-1234"

func newClient(t *testing.T, base string, mutate ...func(*Options)) *Client {
	t.Helper()
	o := Options{BaseURL: base, Token: secret.New(tok), Timeout: 2 * time.Second, MaxResponseBytes: 1 << 10, AllowInsecureHTTP: true}
	for _, m := range mutate {
		m(&o)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGetSendsTokenAndEscapesPath(t *testing.T) {
	var gotURI, gotToken, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI, gotToken, gotUA = r.RequestURI, r.Header.Get("PRIVATE-TOKEN"), r.Header.Get("User-Agent")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	q := url.Values{"ref": {"agent/x y"}}
	resp, err := c.Get(context.Background(), q, "projects", "42", "repository", "files", "src/a b/c#d?.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != `{"ok":true}` || resp.Status != 200 {
		t.Errorf("response: %+v", resp)
	}
	wantURI := "/api/v4/projects/42/repository/files/src%2Fa%20b%2Fc%23d%3F.go?ref=agent%2Fx+y"
	if gotURI != wantURI {
		t.Errorf("request URI = %q, want %q", gotURI, wantURI)
	}
	if gotToken != tok || gotUA != "gitlab-mcp-minimal" {
		t.Errorf("headers: token=%q ua=%q", gotToken, gotUA)
	}
}

func TestBaseURLPrefix(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotURI = r.RequestURI }))
	defer srv.Close()
	c := newClient(t, srv.URL+"/gitlab/")
	if _, err := c.Get(context.Background(), nil, "projects", "1"); err != nil {
		t.Fatal(err)
	}
	if gotURI != "/gitlab/api/v4/projects/1" {
		t.Errorf("request URI = %q", gotURI)
	}
}

func TestRejectsBadSegmentsWithoutRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	c := newClient(t, srv.URL)
	for _, parts := range [][]string{nil, {""}, {"projects", ""}, {"projects", "."}, {"projects", ".."}, {"..", "x"}} {
		if _, err := c.Get(context.Background(), nil, parts...); err == nil {
			t.Errorf("parts %q accepted", parts)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("server was contacted %d times", hits.Load())
	}
}

func TestTraversalLikeSegmentsStayInsideAPIPrefix(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotURI = r.RequestURI }))
	defer srv.Close()
	c := newClient(t, srv.URL)
	for _, seg := range []string{"../../admin", "..%2F..%2Fadmin", "a/../../b", "%2e%2e", "x\r\nHost: evil", "a b"} {
		gotURI = ""
		if _, err := c.Get(context.Background(), nil, "projects", seg); err != nil {
			t.Fatalf("%q: %v", seg, err)
		}
		if !strings.HasPrefix(gotURI, "/api/v4/projects/") || strings.Count(gotURI, "/") != 4 || strings.ContainsAny(gotURI, "\r\n ") {
			t.Errorf("segment %q produced request URI %q", seg, gotURI)
		}
	}
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
		if r.Header.Get("PRIVATE-TOKEN") != "" {
			t.Error("token reached the redirect target")
		}
	}))
	defer evil.Close()
	var selfHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/self") {
			selfHits.Add(1)
			http.Redirect(w, r, srv.URL+"/api/v4/other", http.StatusFound)
			return
		}
		http.Redirect(w, r, evil.URL+"/steal", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	if _, err := c.Get(context.Background(), nil, "projects", "1"); !errors.Is(err, ErrRedirect) {
		t.Errorf("cross-host redirect: err = %v", err)
	}
	if _, err := c.Get(context.Background(), nil, "self"); !errors.Is(err, ErrRedirect) {
		t.Errorf("same-host redirect: err = %v", err)
	}
	if evilHits.Load() != 0 {
		t.Errorf("redirect target was contacted %d times", evilHits.Load())
	}
	if selfHits.Load() != 1 {
		t.Errorf("same-host redirect was followed (hits=%d)", selfHits.Load())
	}
}

func TestResponseSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := 100
		switch r.URL.Path {
		case "/api/v4/exact":
			n = 1 << 10
		case "/api/v4/big":
			n = 1<<10 + 1
		case "/api/v4/chunked":
			// No Content-Length: the limit must be enforced while reading.
			w.(http.Flusher).Flush()
			n = 1 << 20
		}
		io.WriteString(w, strings.Repeat("a", n))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	for _, p := range []string{"small", "exact"} {
		if _, err := c.Get(context.Background(), nil, p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"big", "chunked"} {
		if _, err := c.Get(context.Background(), nil, p); !errors.Is(err, ErrResponseTooLarge) {
			t.Errorf("%s: err = %v", p, err)
		}
	}
}

func TestAPIErrorIsBoundedAndSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/notfound":
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"404 Project Not Found"}`)
		case "/api/v4/obj":
			w.WriteHeader(400)
			io.WriteString(w, `{"message":{"branch_name":["is invalid"]}}`)
		case "/api/v4/evil":
			w.WriteHeader(500)
			io.WriteString(w, `{"error":"bad\u0000\u001b[31m\u202e `+strings.Repeat("x", 5000)+`"}`)
		default:
			w.WriteHeader(502)
			io.WriteString(w, "<html>not json</html>")
		}
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)

	var ae *APIError
	_, err := c.Get(context.Background(), nil, "notfound")
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Message != "404 Project Not Found" {
		t.Errorf("notfound: %v", err)
	}
	_, err = c.Get(context.Background(), nil, "obj")
	if !errors.As(err, &ae) || !strings.Contains(ae.Message, "is invalid") {
		t.Errorf("obj: %v", err)
	}
	_, err = c.Get(context.Background(), nil, "evil")
	if !errors.As(err, &ae) || len(ae.Message) > maxErrorMessage+3 || strings.ContainsAny(ae.Message, "\x00\x1b\u202e") {
		t.Errorf("evil: %q", err)
	}
	_, err = c.Get(context.Background(), nil, "html")
	if !errors.As(err, &ae) || ae.Status != 502 || ae.Message != "" {
		t.Errorf("html: %v", err)
	}
	if strings.Contains(fmt.Sprint(err), tok) {
		t.Error("token in error")
	}
}

func TestPostJSON(t *testing.T) {
	var gotBody, gotCT, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotCT, gotMethod = string(b), r.Header.Get("Content-Type"), r.Method
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":1}`)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	resp, err := c.PostJSON(context.Background(), map[string]any{"branch": "agent/x", "ref": "main"}, "projects", "1", "repository", "branches")
	if err != nil || resp.Status != 201 {
		t.Fatalf("%v %+v", err, resp)
	}
	if gotMethod != "POST" || gotCT != "application/json" || gotBody != `{"branch":"agent/x","ref":"main"}` {
		t.Errorf("method=%s ct=%s body=%s", gotMethod, gotCT, gotBody)
	}
	if _, err := c.PostJSON(context.Background(), make(chan int), "x"); err == nil {
		t.Error("unencodable body accepted")
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	c := newClient(t, srv.URL, func(o *Options) { o.Timeout = 150 * time.Millisecond })
	start := time.Now()
	if _, err := c.Get(context.Background(), nil, "slow"); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("timeout not enforced: %v", time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, nil, "x"); err == nil {
		t.Error("cancelled context accepted")
	}
}

func TestTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	withCA := newClient(t, srv.URL, func(o *Options) { o.AllowInsecureHTTP = false; o.RootCAs = pool })
	if _, err := withCA.Get(context.Background(), nil, "x"); err != nil {
		t.Errorf("trusted CA: %v", err)
	}
	noCA := newClient(t, srv.URL, func(o *Options) { o.AllowInsecureHTTP = false })
	if _, err := noCA.Get(context.Background(), nil, "x"); err == nil {
		t.Error("unknown CA accepted")
	}
}

func TestNewValidation(t *testing.T) {
	ok := Options{BaseURL: "https://gitlab.example.com", Token: secret.New(tok), Timeout: time.Second, MaxResponseBytes: 10}
	if _, err := New(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Options){
		"http without opt-in": func(o *Options) { o.BaseURL = "http://gitlab.example.com" },
		"ftp":                 func(o *Options) { o.BaseURL = "ftp://gitlab.example.com" },
		"no host":             func(o *Options) { o.BaseURL = "https://" },
		"credentials":         func(o *Options) { o.BaseURL = "https://u:p@gitlab.example.com" },
		"query":               func(o *Options) { o.BaseURL = "https://gitlab.example.com?x=1" },
		"empty token":         func(o *Options) { o.Token = secret.Secret{} },
		"zero timeout":        func(o *Options) { o.Timeout = 0 },
		"zero size limit":     func(o *Options) { o.MaxResponseBytes = 0 },
	}
	for name, mutate := range cases {
		o := ok
		mutate(&o)
		if _, err := New(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTokenNotPrintable(t *testing.T) {
	c := newClient(t, "http://127.0.0.1:1")
	if out := fmt.Sprintf("%v %+v %#v", c, c, c); strings.Contains(out, tok) {
		t.Errorf("token leaked: %s", out)
	}
	_, err := c.Get(context.Background(), nil, "x") // connection refused
	if err == nil || strings.Contains(err.Error(), tok) {
		t.Errorf("error must exist and not contain the token: %v", err)
	}
}

func TestLoadCAFile(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/bad.pem"
	if err := writeFile(bad, "not a cert"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCAFile(bad); err == nil {
		t.Error("invalid PEM accepted")
	}
	if _, err := LoadCAFile(dir + "/missing.pem"); err == nil {
		t.Error("missing file accepted")
	}
}
