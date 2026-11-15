package server

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

var commitActions = []string{"create", "update", "delete"}

type fileChange struct {
	Path         string `json:"path" jsonschema:"file path relative to the repository root"`
	Action       string `json:"action" jsonschema:"create, update or delete"`
	Content      string `json:"content,omitempty" jsonschema:"the complete new text content of the file (create and update only)"`
	LastCommitID string `json:"last_commit_id,omitempty" jsonschema:"for update and delete: the last_commit_id returned by get_file; the commit fails if the file changed since"`
}

type commitFilesIn struct {
	Project string       `json:"project" jsonschema:"project ID or path, from list_projects"`
	Branch  string       `json:"branch" jsonschema:"a branch you created, starting with the prefix reported by list_projects"`
	Message string       `json:"message" jsonschema:"the commit message"`
	Files   []fileChange `json:"files" jsonschema:"the changes to commit together"`
}

type commitFilesOut struct {
	CommitID     string `json:"commit_id"`
	Branch       string `json:"branch"`
	FilesChanged int    `json:"files_changed"`
}

// commitAction is the GitLab commit API representation of one change. Content
// is a pointer so that an empty file is still sent.
type commitAction struct {
	Action       string  `json:"action"`
	FilePath     string  `json:"file_path"`
	Content      *string `json:"content,omitempty"`
	LastCommitID string  `json:"last_commit_id,omitempty"`
}

// checkCommit validates every part of a commit request before anything is sent
// to GitLab.
func (s *service) checkCommit(in commitFilesIn) ([]commitAction, error) {
	lim := s.Config.Limits
	if err := validate.RequiredText("message", in.Message, lim.MaxTextBytes, true); err != nil {
		return nil, err
	}
	if len(in.Files) == 0 {
		return nil, validate.Invalid("files", "at least one file change is required")
	}
	if len(in.Files) > lim.MaxFilesPerCommit {
		return nil, deny("a commit may change at most %d files, got %d", lim.MaxFilesPerCommit, len(in.Files))
	}
	seen := make(map[string]bool, len(in.Files))
	actions := make([]commitAction, 0, len(in.Files))
	total := 0
	for _, f := range in.Files {
		if f.Action == "" {
			return nil, validate.Invalid("action", "must be one of: %s", strings.Join(commitActions, ", "))
		}
		if err := validate.OneOf("action", f.Action, commitActions...); err != nil {
			return nil, err
		}
		if err := validate.WritePath(f.Path, s.Config.WriteDenyPaths); err != nil {
			return nil, err
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return nil, validate.Invalid("files", "the path %q appears more than once", f.Path)
		}
		seen[key] = true

		a := commitAction{Action: f.Action, FilePath: f.Path}
		if f.LastCommitID != "" {
			if f.Action == "create" {
				return nil, validate.Invalid("last_commit_id", "is only valid for update and delete")
			}
			if err := validate.CommitSHA("last_commit_id", f.LastCommitID); err != nil {
				return nil, err
			}
			a.LastCommitID = f.LastCommitID
		}
		if f.Action == "delete" {
			if f.Content != "" {
				return nil, validate.Invalid("content", "must be empty for delete")
			}
		} else {
			if !utf8.ValidString(f.Content) || strings.IndexByte(f.Content, 0) >= 0 {
				return nil, validate.Invalid("content", "must be UTF-8 text without NUL bytes; binary files are not supported")
			}
			if len(f.Content) > lim.MaxFileBytes {
				return nil, deny("the content for %s is %d bytes, which exceeds the limit of %d bytes", f.Path, len(f.Content), lim.MaxFileBytes)
			}
			total += len(f.Content)
			content := f.Content
			a.Content = &content
		}
		actions = append(actions, a)
	}
	if total > lim.MaxCommitBytes {
		return nil, deny("the commit is %d bytes of content, which exceeds the limit of %d bytes", total, lim.MaxCommitBytes)
	}
	return actions, nil
}

func (s *service) commitFiles(ctx context.Context, in commitFilesIn) (commitFilesOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return commitFilesOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.Branch}
	if err := validate.AgentBranch(s.Config.BranchPrefix, in.Branch); err != nil {
		return commitFilesOut{}, tgt, err
	}
	actions, err := s.checkCommit(in)
	if err != nil {
		return commitFilesOut{}, tgt, err
	}

	// The branch must exist and must be neither the default nor a protected branch.
	var br struct {
		Protected bool `json:"protected"`
		Default   bool `json:"default"`
	}
	if _, err := s.getJSON(ctx, &br, nil, "projects", idStr(p.ID), "repository", "branches", in.Branch); err != nil {
		return commitFilesOut{}, tgt, err
	}
	if br.Default || br.Protected {
		return commitFilesOut{}, tgt, deny("the branch is the default branch or a protected branch")
	}

	var c struct {
		ID string `json:"id"`
	}
	body := map[string]any{"branch": in.Branch, "commit_message": in.Message, "actions": actions}
	if err := s.postJSON(ctx, &c, body, "projects", idStr(p.ID), "repository", "commits"); err != nil {
		return commitFilesOut{}, tgt, err
	}
	return commitFilesOut{CommitID: c.ID, Branch: in.Branch, FilesChanged: len(actions)}, tgt, nil
}
