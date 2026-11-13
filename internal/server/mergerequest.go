package server

import (
	"context"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

func (s *service) addMergeRequestTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_mr_diff",
		Description: "Read the changes of a merge request as unified diffs, 20 files per page. Large diffs are cut to a size limit.",
		Annotations: readOnly("Read a merge request diff"),
	}, handle(s, "get_mr_diff", s.getMRDiff))
}

type getMRDiffIn struct {
	Project         string `json:"project" jsonschema:"project ID or path, from list_projects"`
	MergeRequestIID int64  `json:"merge_request_iid" jsonschema:"the merge request number within the project (the number after the exclamation mark)"`
	Page            int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type diffFile struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	NewFile     bool   `json:"new_file,omitempty"`
	DeletedFile bool   `json:"deleted_file,omitempty"`
	RenamedFile bool   `json:"renamed_file,omitempty"`
	Diff        string `json:"diff,omitempty" jsonschema:"unified diff hunks for this file"`
	Omitted     bool   `json:"omitted,omitempty" jsonschema:"true when GitLab did not provide the diff because it is too large or collapsed"`
	Truncated   bool   `json:"truncated,omitempty" jsonschema:"true when the diff was cut to fit the size limit"`
}

type getMRDiffOut struct {
	Files     []diffFile `json:"files"`
	Page      int        `json:"page"`
	NextPage  int        `json:"next_page,omitempty"`
	Truncated bool       `json:"truncated" jsonschema:"true when any diff on this page was cut to fit the size limit"`
}

func (s *service) getMRDiff(ctx context.Context, in getMRDiffIn) (getMRDiffOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return getMRDiffOut{}, target{}, err
	}
	tgt := target{project: p.ID}
	if err := validate.ID("merge_request_iid", in.MergeRequestIID); err != nil {
		return getMRDiffOut{}, tgt, err
	}
	page, err := validate.Page(in.Page)
	if err != nil {
		return getMRDiffOut{}, tgt, err
	}
	q := url.Values{"per_page": {strconv.Itoa(smallPageSize)}, "page": {strconv.Itoa(page)}}
	var raw []struct {
		OldPath     string `json:"old_path"`
		NewPath     string `json:"new_path"`
		Diff        string `json:"diff"`
		NewFile     bool   `json:"new_file"`
		RenamedFile bool   `json:"renamed_file"`
		DeletedFile bool   `json:"deleted_file"`
		Collapsed   bool   `json:"collapsed"`
		TooLarge    bool   `json:"too_large"`
	}
	hdr, err := s.getJSON(ctx, &raw, q, "projects", idStr(p.ID), "merge_requests", idStr(in.MergeRequestIID), "diffs")
	if err != nil {
		return getMRDiffOut{}, tgt, err
	}
	out := getMRDiffOut{Files: make([]diffFile, 0, len(raw)), Page: page, NextPage: nextPage(hdr)}
	budget := s.Config.Limits.MaxDiffBytes
	for _, f := range raw {
		df := diffFile{OldPath: f.OldPath, NewPath: f.NewPath, NewFile: f.NewFile, DeletedFile: f.DeletedFile,
			RenamedFile: f.RenamedFile, Omitted: f.Collapsed || f.TooLarge}
		d := f.Diff
		if len(d) > budget {
			d = cutUTF8(d, budget)
			df.Truncated, out.Truncated = true, true
		}
		budget -= len(d)
		df.Diff = d
		out.Files = append(out.Files, df)
	}
	return out, tgt, nil
}

// cutUTF8 shortens s to at most n bytes without splitting a character.
func cutUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
