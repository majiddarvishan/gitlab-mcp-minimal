package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listProjectsIn struct{}

type projectInfo struct {
	ID   int64  `json:"id" jsonschema:"numeric GitLab project ID"`
	Path string `json:"path" jsonschema:"namespaced project path, for example group/project"`
}

type listProjectsOut struct {
	Projects          []projectInfo `json:"projects" jsonschema:"the projects this server can access"`
	WriteBranchPrefix string        `json:"write_branch_prefix" jsonschema:"changes can only be made on branches whose name starts with this prefix"`
}

func (s *service) addListProjects(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Description: "List the GitLab projects this server can access and the branch prefix on which changes are allowed. Call this first.",
		Annotations: &mcp.ToolAnnotations{Title: "List accessible projects", ReadOnlyHint: true, OpenWorldHint: ptr(false)},
	}, handle(s, "list_projects", s.listProjects))
}

func (s *service) listProjects(_ context.Context, _ listProjectsIn) (listProjectsOut, target, error) {
	var out listProjectsOut
	for _, p := range s.Allow.Projects() {
		out.Projects = append(out.Projects, projectInfo{ID: p.ID, Path: p.Path})
	}
	out.WriteBranchPrefix = s.Config.BranchPrefix
	return out, target{}, nil
}
