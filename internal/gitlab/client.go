// Package gitlab is a small, strict HTTP client for the GitLab REST API. It
// talks to exactly one configured instance, never follows redirects, bounds
// every response and builds URLs only from escaped path segments.
package gitlab

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/secret"
)

const (
	apiPrefix        = "/api/v4"
	tokenHeader      = "PRIVATE-TOKEN"
	defaultUserAgent = "gitlab-mcp-minimal"
	maxErrorBody     = 4 << 10
	maxErrorMessage  = 200
)

// Errors returned by the client.
var (
	ErrRedirect         = errors.New("gitlab: redirect refused")
	ErrResponseTooLarge = errors.New("gitlab: response exceeds the configured size limit")
)

// APIError is a non-2xx response from GitLab.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("gitlab: HTTP %d", e.Status)
	}
	return fmt.Sprintf("gitlab: HTTP %d: %s", e.Status, e.Message)
}

// Options configures a Client.
type Options struct {
	BaseURL           string // instance root, see config.Config.BaseURL
	Token             secret.Secret
	Timeout           time.Duration
	MaxResponseBytes  int64
	AllowInsecureHTTP bool
	RootCAs           *x509.CertPool // nil means the system pool
	UserAgent         string
}

// Client calls the GitLab REST API.
type Client struct {
	root    *url.URL
	token   secret.Secret
	hc      *http.Client
	maxResp int64
	ua      string
}

// Response is a successful (2xx) response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// LoadCAFile reads a PEM bundle into a certificate pool.
func LoadCAFile(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gitlab: read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("gitlab: CA file contains no valid certificates")
	}
	return pool, nil
}

// New validates o and returns a Client.
func New(o Options) (*Client, error) {
	root, err := url.Parse(o.BaseURL)
	if err != nil || root.Host == "" || root.User != nil || root.RawQuery != "" || root.Fragment != "" {
		return nil, errors.New("gitlab: invalid base URL")
	}
	switch {
	case root.Scheme == "https":
	case root.Scheme == "http" && o.AllowInsecureHTTP:
	default:
		return nil, errors.New("gitlab: base URL must use https")
	}
	if o.Token.IsZero() {
		return nil, errors.New("gitlab: token is required")
	}
	if o.Timeout <= 0 {
		return nil, errors.New("gitlab: timeout must be positive")
	}
	if o.MaxResponseBytes <= 0 {
		return nil, errors.New("gitlab: response size limit must be positive")
	}
	ua := o.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	tr := &http.Transport{
		// No proxy: the token must only ever travel directly to the configured host.
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.RootCAs},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: o.Timeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Client{
		root:  root,
		token: o.Token,
		hc: &http.Client{
			Transport: tr,
			Timeout:   o.Timeout,
			// Go forwards custom headers such as PRIVATE-TOKEN across redirects,
			// so redirects are never followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		maxResp: o.MaxResponseBytes,
		ua:      ua,
	}, nil
}

// Get performs GET {base}/api/v4/{parts...}?{query}. Each part is a single raw
// path segment and is escaped here (a part containing "/" becomes %2F).
func (c *Client) Get(ctx context.Context, query url.Values, parts ...string) (*Response, error) {
	return c.do(ctx, http.MethodGet, query, nil, parts)
}

// PostJSON performs POST with body encoded as JSON.
func (c *Client) PostJSON(ctx context.Context, body any, parts ...string) (*Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gitlab: encode request: %w", err)
	}
	return c.do(ctx, http.MethodPost, nil, b, parts)
}

func (c *Client) endpoint(query url.Values, parts []string) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("gitlab: empty request path")
	}
	raw := strings.TrimRight(c.root.EscapedPath(), "/") + apiPrefix
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", errors.New("gitlab: invalid path segment")
		}
		raw += "/" + url.PathEscape(p)
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", errors.New("gitlab: invalid path")
	}
	u := *c.root
	u.Path, u.RawPath = decoded, raw
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *Client) do(ctx context.Context, method string, query url.Values, body []byte, parts []string) (*Response, error) {
	resp, err := c.send(ctx, method, query, body, parts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > c.maxResp {
		return nil, ErrResponseTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResp+1))
	if err != nil {
		return nil, fmt.Errorf("gitlab: read response: %w", err)
	}
	if int64(len(b)) > c.maxResp {
		return nil, ErrResponseTooLarge
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

// send builds and sends a request and maps redirects and error statuses to
// errors. On success the caller must close resp.Body.
func (c *Client) send(ctx context.Context, method string, query url.Values, body []byte, parts []string) (*http.Response, error) {
	target, err := c.endpoint(query, parts)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, errors.New("gitlab: cannot build request")
	}
	req.Header.Set(tokenHeader, c.token.Reveal())
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json, text/plain;q=0.9")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: request failed: %w", err)
	}
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		resp.Body.Close()
		return nil, ErrRedirect
	case resp.StatusCode >= 400:
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &APIError{Status: resp.StatusCode, Message: errorMessage(b)}
	}
	return resp, nil
}

// Tail is the end of a response body.
type Tail struct {
	Data      []byte // at most the requested number of final bytes
	Total     int64  // bytes in the whole body
	Truncated bool   // whether earlier bytes were dropped
}

// GetTail performs a GET like Get, but for large text bodies such as job logs:
// it streams the body, keeps only the last keep bytes, and fails with
// ErrResponseTooLarge when the body exceeds maxScan bytes.
func (c *Client) GetTail(ctx context.Context, query url.Values, keep int, maxScan int64, parts ...string) (*Tail, error) {
	if keep <= 0 || maxScan < int64(keep) {
		return nil, errors.New("gitlab: invalid tail limits")
	}
	resp, err := c.send(ctx, http.MethodGet, query, nil, parts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var (
		data  []byte
		total int64
		chunk = make([]byte, 32<<10)
	)
	for {
		n, rerr := resp.Body.Read(chunk)
		total += int64(n)
		if total > maxScan {
			return nil, ErrResponseTooLarge
		}
		data = append(data, chunk[:n]...)
		if len(data) > 2*keep {
			data = append(data[:0], data[len(data)-keep:]...)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("gitlab: read response: %w", rerr)
		}
	}
	if len(data) > keep {
		data = append([]byte(nil), data[len(data)-keep:]...)
	}
	return &Tail{Data: data, Total: total, Truncated: total > int64(keep)}, nil
}

// errorMessage extracts a short, printable message from a GitLab error body.
func errorMessage(b []byte) string {
	var v struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	msg := ""
	if json.Unmarshal(b, &v) == nil {
		switch m := v.Message.(type) {
		case string:
			msg = m
		case nil:
			msg = v.Error
		default:
			if enc, err := json.Marshal(m); err == nil {
				msg = string(enc)
			}
		}
	}
	return sanitize(msg)
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > maxErrorMessage {
		s = s[:maxErrorMessage]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
		s += "..."
	}
	return strings.TrimSpace(s)
}
