package server

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/majiddarvishan/gitlab-mcp-minimal/internal/validate"
)

// traceScanLimit bounds how much of a job log is streamed to find its tail.
const traceScanLimit = 32 << 20

var pipelineStatuses = []string{
	"created", "waiting_for_resource", "preparing", "pending", "running", "success",
	"failed", "canceled", "skipped", "manual", "scheduled",
}

func (s *service) addCITools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pipelines",
		Description: "List a project's CI pipelines, newest first, optionally filtered by ref or status. Returns 20 per page.",
		Annotations: readOnly("List pipelines"),
	}, handle(s, "list_pipelines", s.listPipelines))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pipeline_jobs",
		Description: "List the jobs of one pipeline. Use a job id with get_job_trace to read its log.",
		Annotations: readOnly("List pipeline jobs"),
	}, handle(s, "list_pipeline_jobs", s.listPipelineJobs))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_job_trace",
		Description: "Read the end of a CI job log, with terminal escape codes removed. Long logs are cut to their last part.",
		Annotations: readOnly("Read a job log"),
	}, handle(s, "get_job_trace", s.getJobTrace))
}

type listPipelinesIn struct {
	Project string `json:"project" jsonschema:"project ID or path, from list_projects"`
	Ref     string `json:"ref,omitempty" jsonschema:"only pipelines for this branch or tag"`
	Status  string `json:"status,omitempty" jsonschema:"only pipelines with this status"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type pipelineInfo struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Ref       string `json:"ref"`
	SHA       string `json:"sha"`
	Source    string `json:"source,omitempty" jsonschema:"what triggered the pipeline, for example push or merge_request_event"`
	CreatedAt string `json:"created_at,omitempty"`
}

type listPipelinesOut struct {
	Pipelines []pipelineInfo `json:"pipelines"`
	Page      int            `json:"page"`
	NextPage  int            `json:"next_page,omitempty"`
}

func (s *service) listPipelines(ctx context.Context, in listPipelinesIn) (listPipelinesOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return listPipelinesOut{}, target{}, err
	}
	tgt := target{project: p.ID, ref: in.Ref}
	if in.Ref != "" {
		if err := validate.Ref(in.Ref); err != nil {
			return listPipelinesOut{}, tgt, err
		}
	}
	if err := validate.OneOf("status", in.Status, pipelineStatuses...); err != nil {
		return listPipelinesOut{}, tgt, err
	}
	page, err := validate.Page(in.Page)
	if err != nil {
		return listPipelinesOut{}, tgt, err
	}
	q := url.Values{"per_page": {strconv.Itoa(smallPageSize)}, "page": {strconv.Itoa(page)}, "order_by": {"id"}, "sort": {"desc"}}
	if in.Ref != "" {
		q.Set("ref", in.Ref)
	}
	if in.Status != "" {
		q.Set("status", in.Status)
	}
	var items []pipelineInfo
	hdr, err := s.getJSON(ctx, &items, q, "projects", idStr(p.ID), "pipelines")
	if err != nil {
		return listPipelinesOut{}, tgt, err
	}
	if items == nil {
		items = []pipelineInfo{}
	}
	return listPipelinesOut{Pipelines: items, Page: page, NextPage: nextPage(hdr)}, tgt, nil
}

type listPipelineJobsIn struct {
	Project    string `json:"project" jsonschema:"project ID or path, from list_projects"`
	PipelineID int64  `json:"pipeline_id" jsonschema:"pipeline id from list_pipelines"`
	Page       int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
}

type jobInfo struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Stage           string   `json:"stage"`
	Status          string   `json:"status"`
	AllowFailure    bool     `json:"allow_failure"`
	FailureReason   string   `json:"failure_reason,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
}

type listPipelineJobsOut struct {
	Jobs     []jobInfo `json:"jobs"`
	Page     int       `json:"page"`
	NextPage int       `json:"next_page,omitempty"`
}

func (s *service) listPipelineJobs(ctx context.Context, in listPipelineJobsIn) (listPipelineJobsOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return listPipelineJobsOut{}, target{}, err
	}
	tgt := target{project: p.ID}
	if err := validate.ID("pipeline_id", in.PipelineID); err != nil {
		return listPipelineJobsOut{}, tgt, err
	}
	page, err := validate.Page(in.Page)
	if err != nil {
		return listPipelineJobsOut{}, tgt, err
	}
	q := url.Values{"per_page": {strconv.Itoa(smallPageSize)}, "page": {strconv.Itoa(page)}}
	var raw []struct {
		ID            int64    `json:"id"`
		Name          string   `json:"name"`
		Stage         string   `json:"stage"`
		Status        string   `json:"status"`
		AllowFailure  bool     `json:"allow_failure"`
		FailureReason string   `json:"failure_reason"`
		Duration      *float64 `json:"duration"`
	}
	hdr, err := s.getJSON(ctx, &raw, q, "projects", idStr(p.ID), "pipelines", idStr(in.PipelineID), "jobs")
	if err != nil {
		return listPipelineJobsOut{}, tgt, err
	}
	out := listPipelineJobsOut{Jobs: make([]jobInfo, 0, len(raw)), Page: page, NextPage: nextPage(hdr)}
	for _, j := range raw {
		out.Jobs = append(out.Jobs, jobInfo{ID: j.ID, Name: j.Name, Stage: j.Stage, Status: j.Status,
			AllowFailure: j.AllowFailure, FailureReason: j.FailureReason, DurationSeconds: j.Duration})
	}
	return out, tgt, nil
}

type getJobTraceIn struct {
	Project string `json:"project" jsonschema:"project ID or path, from list_projects"`
	JobID   int64  `json:"job_id" jsonschema:"job id from list_pipeline_jobs"`
}

type getJobTraceOut struct {
	JobID      int64  `json:"job_id"`
	Log        string `json:"log" jsonschema:"the job log, cleaned of terminal escape codes"`
	Truncated  bool   `json:"truncated" jsonschema:"true when the beginning of the log was cut off"`
	TotalBytes int64  `json:"total_bytes" jsonschema:"size of the full raw log in bytes"`
}

func (s *service) getJobTrace(ctx context.Context, in getJobTraceIn) (getJobTraceOut, target, error) {
	p, err := s.Allow.Resolve(in.Project)
	if err != nil {
		return getJobTraceOut{}, target{}, err
	}
	tgt := target{project: p.ID}
	if err := validate.ID("job_id", in.JobID); err != nil {
		return getJobTraceOut{}, tgt, err
	}
	tail, err := s.GitLab.GetTail(ctx, nil, s.Config.Limits.MaxTraceBytes, traceScanLimit,
		"projects", idStr(p.ID), "jobs", idStr(in.JobID), "trace")
	if err != nil {
		return getJobTraceOut{}, tgt, err
	}
	return getJobTraceOut{JobID: in.JobID, Log: cleanLog(tail.Data, tail.Truncated), Truncated: tail.Truncated, TotalBytes: tail.Total}, tgt, nil
}

var (
	ansiRE    = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-9A-Za-z]`)
	sectionRE = regexp.MustCompile(`section_(?:start|end):\d+:[^\r\n]*`)
	blankRE   = regexp.MustCompile(`\n{3,}`)
)

// cleanLog turns raw runner output into plain text: no escape codes, section
// markers or stray control characters, and no partial first line when the
// beginning was cut off.
func cleanLog(b []byte, truncated bool) string {
	str := strings.ToValidUTF8(string(b), "\uFFFD")
	if truncated {
		if i := strings.IndexByte(str, '\n'); i >= 0 {
			str = str[i+1:]
		}
	}
	str = sectionRE.ReplaceAllString(str, "")
	str = ansiRE.ReplaceAllString(str, "")
	str = strings.ReplaceAll(strings.ReplaceAll(str, "\r\n", "\n"), "\r", "\n")
	str = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, str)
	return blankRE.ReplaceAllString(str, "\n\n")
}
