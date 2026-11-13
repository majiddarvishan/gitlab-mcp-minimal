package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: ptr(false)}
}

func (s *service) addRepositoryTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_tree",
		Description: "List files and directories in a project's repository. Returns at most 100 entries per page; use next_page to continue.",
		Annotations: readOnly("List repository files"),
	}, handle(s, "get_tree", s.getTree))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_file",
		Description: "Read one text file from a project's repository. Binary files and files above the size limit are not returned.",
		Annotations: readOnly("Read a file"),
	}, handle(s, "get_file", s.getFile))
}

type getTreeIn struct {
	Project   string `json:"project" jsonschema:"project ID or path, from list_projects"`
	Path      string `json:"path,omitempty" jsonschema:"directory to list, relative to the repository root (default: the root)"`
	Ref       string `json:"ref,omitempty" jsonschema:"branch, tag or commit SHA (default: the default branch)"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"list everything below the directory instead of only its direct children"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type treeEntry struct {
	Path string `json:"path" jsonschema:"path relative to the repository root"`
	Type string `json:"type" jsonschema:"file, dir or submodule"`
}

type getTreeOut struct {
	Entries  []treeEntry `json:"entries" jsonschema:"entries on this page"`
	Page     int         `json:"page" jsonschema:"the page returned"`
	NextPage int         `json:"next_page,omitempty" jsonschema:"the next page number, absent on the last page"`
}

func (s *service) getTree(ctx context.Context, in getTreeIn) (getTreeOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return getTreeOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.Ref}
	if in.Path != "" {
		if err := validate.FilePath(in.Path); err != nil {
			return getTreeOut{}, tgt, err
		}
	}
	if in.Ref != "" {
		if err := validate.Ref(in.Ref); err != nil {
			return getTreeOut{}, tgt, err
		}
	}
	page, err := validate.Page(in.Page)
	if err != nil {
		return getTreeOut{}, tgt, err
	}

	q := url.Values{"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
	if in.Path != "" {
		q.Set("path", in.Path)
	}
	if in.Ref != "" {
		q.Set("ref", in.Ref)
	}
	if in.Recursive {
		q.Set("recursive", "true")
	}
	var items []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	hdr, err := s.getJSON(ctx, &items, q, "projects", idStr(p.ID), "repository", "tree")
	if err != nil {
		return getTreeOut{}, tgt, err
	}
	out := getTreeOut{Page: page, NextPage: nextPage(hdr), Entries: make([]treeEntry, 0, len(items))}
	for _, it := range items {
		out.Entries = append(out.Entries, treeEntry{Path: it.Path, Type: entryType(it.Type)})
	}
	return out, tgt, nil
}

func entryType(t string) string {
	switch t {
	case "blob":
		return "file"
	case "tree":
		return "dir"
	case "commit":
		return "submodule"
	}
	return t
}

type getFileIn struct {
	Project string `json:"project" jsonschema:"project ID or path, from list_projects"`
	Path    string `json:"path" jsonschema:"file path relative to the repository root"`
	Ref     string `json:"ref,omitempty" jsonschema:"branch, tag or commit SHA (default: the default branch)"`
}

type getFileOut struct {
	Path         string `json:"path"`
	Ref          string `json:"ref" jsonschema:"the ref the file was read from"`
	Size         int64  `json:"size" jsonschema:"file size in bytes"`
	Binary       bool   `json:"binary" jsonschema:"true when the file is not text; its content is not returned"`
	Content      string `json:"content,omitempty" jsonschema:"the file's text content"`
	BlobID       string `json:"blob_id,omitempty"`
	LastCommitID string `json:"last_commit_id,omitempty" jsonschema:"the last commit that changed this file"`
}

func (s *service) getFile(ctx context.Context, in getFileIn) (getFileOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return getFileOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.Ref}
	if err := validate.FilePath(in.Path); err != nil {
		return getFileOut{}, tgt, err
	}
	if in.Ref != "" {
		if err := validate.Ref(in.Ref); err != nil {
			return getFileOut{}, tgt, err
		}
	}
	ref, err := s.refOrDefault(ctx, p.ID, in.Ref)
	if err != nil {
		return getFileOut{}, tgt, err
	}
	tgt.ref = ref

	var f struct {
		Size         int64  `json:"size"`
		Encoding     string `json:"encoding"`
		Content      string `json:"content"`
		BlobID       string `json:"blob_id"`
		LastCommitID string `json:"last_commit_id"`
	}
	q := url.Values{"ref": {ref}}
	if _, err := s.getJSON(ctx, &f, q, "projects", idStr(p.ID), "repository", "files", in.Path); err != nil {
		return getFileOut{}, tgt, err
	}
	if f.Encoding != "base64" {
		return getFileOut{}, tgt, errors.New("unexpected file encoding from GitLab: " + f.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(f.Content))
	if err != nil {
		return getFileOut{}, tgt, errors.New("GitLab returned invalid base64 file content")
	}
	if max := s.Config.Limits.MaxFileBytes; len(data) > max {
		return getFileOut{}, tgt, deny("the file is %d bytes, which exceeds the limit of %d bytes", len(data), max)
	}
	out := getFileOut{Path: in.Path, Ref: ref, Size: int64(len(data)), BlobID: f.BlobID, LastCommitID: f.LastCommitID}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		out.Binary = true
		return out, tgt, nil
	}
	out.Content = string(data)
	return out, tgt, nil
}
