# gitlab-mcp-minimal

A minimal [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server in Go that lets an AI assistant
read repositories and propose changes on a self-hosted GitLab instance, through branches and merge requests, and
nothing else.

> **Status: work in progress (pre-release).** The server runs and exposes seven **read-only** tools. The tools that
> create branches, commit files and open draft merge requests are not implemented yet (Phase 3). Do not point this at
> a production GitLab until the security review in Phase 5 is done.

## Why another GitLab MCP server?

General-purpose GitLab MCP servers expose a large surface (GraphQL, uploads, several transports, per-request URLs),
and that surface is where past vulnerabilities in this category came from. This project takes the opposite
approach: a handful of tools, one transport, one configured instance, and every rule enforced in code.

## What it does and does not do

| Does | Does not |
|---|---|
| Read files, trees, merge request diffs, pipelines and job logs of allow-listed projects | Merge merge requests |
| Create branches under a configured prefix (default `agent/`) *(planned, Phase 3)* | Write to default or protected branches |
| Commit files to those branches, with size limits and a write deny-list *(planned, Phase 3)* | Touch CI configuration, `CODEOWNERS` or `.gitlab/` (deny-listed by default) |
| Open draft merge requests and add notes *(planned, Phase 3)* | Run GraphQL or arbitrary API calls, upload files, read local files |
| Talk to exactly one configured GitLab instance over stdio | Listen on any network port |

## Tools (current)

All are annotated read-only and closed-world, so MCP clients such as ChatGPT do not need to ask for confirmation.

| Tool | Purpose |
|---|---|
| `list_projects` | The allow-listed projects and the branch prefix on which changes will be allowed |
| `get_tree` | List files and directories (100 per page) |
| `get_file` | Read one text file; binary and oversized files are not returned |
| `get_mr_diff` | Merge request diffs, cut to a size limit |
| `list_pipelines`, `list_pipeline_jobs` | CI pipelines and their jobs |
| `get_job_trace` | The end of a job log, cleaned of terminal escape codes |

Tool output never contains links to your GitLab instance, and error messages shown to the model are short and fixed;
details go to the audit log on stderr only.

## Architecture

```
ChatGPT -> OpenAI-hosted tunnel endpoint <- outbound HTTPS <- tunnel-client (inside your network)
                                                              |
                                          this server (stdio child process) -> GitLab REST API
```

`tunnel-client` is OpenAI's open-source client for its Secure MCP Tunnel; it starts this server as a child process.
stdout carries the MCP protocol, so all logs (one JSON line per tool call) go to stderr.

## Build and run

```sh
go build -o gitlab-mcp-minimal ./cmd/gitlab-mcp-minimal
GITLAB_TOKEN=... ./gitlab-mcp-minimal -config config.json    # or set GITLAB_MCP_CONFIG
```

The process speaks MCP on stdin/stdout, so it is normally started by an MCP client or by `tunnel-client`, not by hand.
`-version` prints the version. The first time after cloning, run `go mod tidy` so that `go.sum` is generated against
the Go checksum database.

## Configuration

See [`config.example.json`](config.example.json). The configuration never contains the token itself, only where to
read it (`token_file`, preferred, or `token_env`).

| Field | Meaning |
|---|---|
| `base_url` | GitLab instance root. `https` only unless `allow_insecure_http` is set |
| `ca_file` | Optional absolute path to a PEM bundle for a private CA |
| `token_file` / `token_env` | Exactly one; where to read the GitLab token |
| `projects` | Allow-list of `{id, path}`. Requests are sent by numeric ID, never by user-supplied path |
| `branch_prefix` | Branches the agent may write to (default `agent/`) |
| `write_deny_paths` | Paths the agent may never write (default `.gitlab-ci.yml`, `.gitlab`, `CODEOWNERS`) |
| `request_timeout_seconds`, `limits` | Timeouts and size caps (files, commits, responses, diffs, logs), with hard ceilings |

Unknown fields are rejected.

## GitLab-side setup (required)

The in-server checks are defence in depth. The real boundary is GitLab itself: use a dedicated bot user or project
access token with the Developer role, short expiry, protect the default branch against direct pushes, put CI
configuration under CODEOWNERS, and keep CI secrets as protected variables. Details in
[`.claude/DECISIONS.md`](.claude/DECISIONS.md). The merge request diff tool uses the `/diffs` endpoint, which needs
GitLab 15.7 or newer.

## Development

```sh
go vet ./...
go test -race ./...
go test ./internal/validate -fuzz FuzzFilePath -fuzztime 30s   # also FuzzBranchName, FuzzProjectPath, FuzzText, FuzzResolve
```

Project context, design decisions and the task plan live in [`.claude/`](.claude/). Security reports: see
[`SECURITY.md`](SECURITY.md).
