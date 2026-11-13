package gitlab

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTail(t *testing.T) {
	body := make([]byte, 100_000)
	for i := range body {
		body[i] = byte('a' + i%26)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/small":
			io.WriteString(w, "hello")
		case "/api/v4/missing":
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"404 Not Found"}`)
		default:
			w.(http.Flusher).Flush() // no Content-Length
			w.Write(body)
		}
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)

	got, err := c.GetTail(context.Background(), nil, 1000, 1<<20, "big")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Total != int64(len(body)) || len(got.Data) != 1000 || string(got.Data) != string(body[len(body)-1000:]) {
		t.Errorf("tail wrong: truncated=%v total=%d len=%d", got.Truncated, got.Total, len(got.Data))
	}

	got, err = c.GetTail(context.Background(), nil, 1000, 1<<20, "small")
	if err != nil || got.Truncated || string(got.Data) != "hello" || got.Total != 5 {
		t.Errorf("small body: %+v %v", got, err)
	}

	if _, err := c.GetTail(context.Background(), nil, 1000, 50_000, "big"); !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("scan limit not enforced: %v", err)
	}
	var ae *APIError
	if _, err := c.GetTail(context.Background(), nil, 1000, 1<<20, "missing"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("error status not mapped: %v", err)
	}
	for _, args := range [][2]int64{{0, 10}, {-1, 10}, {100, 50}} {
		if _, err := c.GetTail(context.Background(), nil, int(args[0]), args[1], "small"); err == nil {
			t.Errorf("limits %v accepted", args)
		}
	}
}
