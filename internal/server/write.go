package server

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

// writeTool annotates a tool that changes GitLab. Without ReadOnlyHint, clients
// such as ChatGPT ask the user to confirm each call.
func writeTool(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: ptr(destructive), OpenWorldHint: ptr(false)}
}

func (s *service) addWriteTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_branch",
		Description: "Create a new branch for your changes. The name must start with the prefix reported by list_projects.",
		Annotations: writeTool("Create a branch", false),
	}, handle(s, "create_branch", s.createBranch))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "commit_files",
		Description: "Commit text file changes (create, update or delete) to a branch you created, as one commit. Default and protected branches and CI configuration cannot be changed.",
		Annotations: writeTool("Commit file changes", true),
	}, handle(s, "commit_files", s.commitFiles))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_merge_request",
		Description: "Open a draft merge request from one of your branches into the project's default branch. A human reviews and merges it.",
		Annotations: writeTool("Open a draft merge request", false),
	}, handle(s, "create_merge_request", s.createMergeRequest))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "add_mr_note",
		Description: "Add a comment to an open merge request that comes from one of your branches.",
		Annotations: writeTool("Comment on a merge request", false),
	}, handle(s, "add_mr_note", s.addMRNote))
}

type createBranchIn struct {
	Project string `json:"project" jsonschema:"project ID or path, from list_projects"`
	Branch  string `json:"branch" jsonschema:"the new branch name, starting with the prefix reported by list_projects"`
	From    string `json:"from,omitempty" jsonschema:"branch, tag or commit SHA to start from (default: the default branch)"`
}

type createBranchOut struct {
	Branch   string `json:"branch"`
	From     string `json:"from"`
	CommitID string `json:"commit_id" jsonschema:"the commit the new branch points at"`
}

func (s *service) createBranch(ctx context.Context, in createBranchIn) (createBranchOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return createBranchOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.Branch}
	if err := validate.AgentBranch(s.Config.BranchPrefix, in.Branch); err != nil {
		return createBranchOut{}, tgt, err
	}
	if in.From != "" {
		if err := validate.Ref(in.From); err != nil {
			return createBranchOut{}, tgt, err
		}
	}
	from, err := s.refOrDefault(ctx, p.ID, in.From)
	if err != nil {
		return createBranchOut{}, tgt, err
	}
	var br struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	body := map[string]string{"branch": in.Branch, "ref": from}
	if err := s.postJSON(ctx, &br, body, "projects", idStr(p.ID), "repository", "branches"); err != nil {
		return createBranchOut{}, tgt, err
	}
	return createBranchOut{Branch: in.Branch, From: from, CommitID: br.Commit.ID}, tgt, nil
}

type createMergeRequestIn struct {
	Project      string `json:"project" jsonschema:"project ID or path, from list_projects"`
	SourceBranch string `json:"source_branch" jsonschema:"your branch with the changes"`
	Title        string `json:"title" jsonschema:"merge request title; it is always created as a draft"`
	Description  string `json:"description,omitempty" jsonschema:"what changed and why"`
}

type createMergeRequestOut struct {
	IID          int64  `json:"iid" jsonschema:"the merge request number within the project"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

var draftPrefixes = []string{"draft:", "[draft]", "(draft)"}

func isDraftTitle(t string) bool {
	l := strings.ToLower(strings.TrimSpace(t))
	for _, p := range draftPrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// draftTitle makes sure a merge request title carries a draft prefix, which is
// how GitLab marks a merge request as a draft.
func draftTitle(t string) string {
	t = strings.TrimSpace(t)
	if isDraftTitle(t) {
		return t
	}
	return "Draft: " + t
}

func (s *service) createMergeRequest(ctx context.Context, in createMergeRequestIn) (createMergeRequestOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return createMergeRequestOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.SourceBranch}
	if err := validate.AgentBranch(s.Config.BranchPrefix, in.SourceBranch); err != nil {
		return createMergeRequestOut{}, tgt, err
	}
	if err := validate.Title(in.Title); err != nil {
		return createMergeRequestOut{}, tgt, err
	}
	title := draftTitle(in.Title)
	if len(title) > validate.MaxTitleLen {
		return createMergeRequestOut{}, tgt, validate.Invalid("title", "too long once the Draft prefix is added (max %d bytes)", validate.MaxTitleLen)
	}
	if err := validate.Text("description", in.Description, s.Config.Limits.MaxTextBytes, true); err != nil {
		return createMergeRequestOut{}, tgt, err
	}
	targetBranch, err := s.refOrDefault(ctx, p.ID, "")
	if err != nil {
		return createMergeRequestOut{}, tgt, err
	}
	if in.SourceBranch == targetBranch {
		return createMergeRequestOut{}, tgt, deny("the source branch is the default branch")
	}
	var mr struct {
		IID          int64  `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
	}
	body := map[string]string{"source_branch": in.SourceBranch, "target_branch": targetBranch, "title": title, "description": in.Description}
	if err := s.postJSON(ctx, &mr, body, "projects", idStr(p.ID), "merge_requests"); err != nil {
		return createMergeRequestOut{}, tgt, err
	}
	return createMergeRequestOut{IID: mr.IID, Title: mr.Title, State: mr.State, Draft: isDraftTitle(mr.Title),
		SourceBranch: mr.SourceBranch, TargetBranch: mr.TargetBranch}, tgt, nil
}

type addMRNoteIn struct {
	Project         string `json:"project" jsonschema:"project ID or path, from list_projects"`
	MergeRequestIID int64  `json:"merge_request_iid" jsonschema:"the merge request number within the project"`
	Body            string `json:"body" jsonschema:"the comment text"`
}

type addMRNoteOut struct {
	NoteID          int64 `json:"note_id"`
	MergeRequestIID int64 `json:"merge_request_iid"`
}

func (s *service) addMRNote(ctx context.Context, in addMRNoteIn) (addMRNoteOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return addMRNoteOut{}, target{}, err
	}
	tgt := target{project: p.ID}
	if err := validate.ID("merge_request_iid", in.MergeRequestIID); err != nil {
		return addMRNoteOut{}, tgt, err
	}
	if err := validate.RequiredText("body", in.Body, s.Config.Limits.MaxTextBytes, true); err != nil {
		return addMRNoteOut{}, tgt, err
	}
	var mr struct {
		SourceBranch string `json:"source_branch"`
		State        string `json:"state"`
	}
	iid := idStr(in.MergeRequestIID)
	if _, err := s.getJSON(ctx, &mr, nil, "projects", idStr(p.ID), "merge_requests", iid); err != nil {
		return addMRNoteOut{}, tgt, err
	}
	tgt.ref = mr.SourceBranch
	if validate.AgentBranch(s.Config.BranchPrefix, mr.SourceBranch) != nil {
		return addMRNoteOut{}, tgt, deny("comments are only allowed on merge requests from branches starting with %s", s.Config.BranchPrefix)
	}
	if mr.State != "opened" {
		return addMRNoteOut{}, tgt, deny("the merge request is %s, not open", mr.State)
	}
	var note struct {
		ID int64 `json:"id"`
	}
	if err := s.postJSON(ctx, &note, map[string]string{"body": in.Body}, "projects", idStr(p.ID), "merge_requests", iid, "notes"); err != nil {
		return addMRNoteOut{}, tgt, err
	}
	return addMRNoteOut{NoteID: note.ID, MergeRequestIID: in.MergeRequestIID}, tgt, nil
}
