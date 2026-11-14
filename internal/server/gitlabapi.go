package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

const (
	pageSize         = 100 // entries per page for tree listings
	smallPageSize    = 20  // entries per page for heavier listings (pipelines, jobs, diffs)
	defaultBranchTTL = 5 * time.Minute
)

type cachedBranch struct {
	name string
	at   time.Time
}

type branchCache struct {
	mu sync.Mutex
	m  map[int64]cachedBranch
}

func (c *branchCache) get(id int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[id]; ok && time.Since(e.at) < defaultBranchTTL {
		return e.name, true
	}
	return "", false
}

func (c *branchCache) put(id int64, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[int64]cachedBranch{}
	}
	c.m[id] = cachedBranch{name: name, at: time.Now()}
}

func idStr(id int64) string { return strconv.FormatInt(id, 10) }

// getJSON performs a GET and decodes the JSON body into dst.
func (s *service) getJSON(ctx context.Context, dst any, query url.Values, parts ...string) (http.Header, error) {
	resp, err := s.GitLab.Get(ctx, query, parts...)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resp.Body, dst); err != nil {
		return nil, fmt.Errorf("decode GitLab response: %w", err)
	}
	return resp.Header, nil
}

// nextPage reads GitLab's X-Next-Page header; 0 means there is no further page.
func nextPage(h http.Header) int {
	n, err := strconv.Atoi(h.Get("X-Next-Page"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// refOrDefault returns ref when given, otherwise the project's default branch.
func (s *service) refOrDefault(ctx context.Context, projectID int64, ref string) (string, error) {
	if ref != "" {
		return ref, nil
	}
	if name, ok := s.branches.get(projectID); ok {
		return name, nil
	}
	var meta struct {
		DefaultBranch *string `json:"default_branch"`
	}
	if _, err := s.getJSON(ctx, &meta, nil, "projects", idStr(projectID)); err != nil {
		return "", err
	}
	if meta.DefaultBranch == nil || *meta.DefaultBranch == "" {
		return "", errors.New("the project has no default branch; pass ref explicitly")
	}
	if err := validate.Ref(*meta.DefaultBranch); err != nil {
		return "", deny("the project's default branch has a name this server does not support; pass ref explicitly")
	}
	s.branches.put(projectID, *meta.DefaultBranch)
	return *meta.DefaultBranch, nil
}
