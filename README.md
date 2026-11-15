# gitlab-mcp-minimal

A minimal [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server in Go that lets an AI assistant
read repositories and propose changes on a self-hosted GitLab instance, through branches and merge requests, and
nothing else.

> **Status: work in progress (pre-release).** The server runs with seven read-only tools and, when you opt in with
> `enable_write_tools`, four write tools. Packaging and deployment guides exist (Docker image on top of OpenAI's
> `tunnel-client-runtime`), but it has only been tested against a fake GitLab so far; the end-to-end test on a real
> instance ([`docs/e2e-test.md`](docs/e2e-test.md)) and the security review are still to do. Do not point it at a
> production GitLab yet.

## Why another GitLab MCP server?

General-purpose GitLab MCP servers expose a large surface (GraphQL, uploads, several transports, per-request URLs),
and that surface is where past vulnerabilities in this category came from. This project takes the opposite
approach: a handful of tools, one transport, one configured instance, and every rule enforced in code.

## What it does and does not do

| Does | Does not |
|---|---|
| Read files, trees, merge request diffs, pipelines and job logs of allow-listed projects | Merge merge requests |
| Create branches under a configured prefix (default `agent/`) | Write to default or protected branches |
| Commit text files to those branches, with size limits and a write deny-list | Touch CI configuration, `CODEOWNERS` or `.gitlab/` (deny-listed by default) |
| Open draft merge requests and comment on its own merge requests | Run GraphQL or arbitrary API calls, upload files, read local files, write binary files |
| Talk to exactly one configured GitLab instance over stdio | Listen on any network port |

The write tools exist only when `enable_write_tools` is set to `true`; by default the server is read-only.

## Tools

Read tools are annotated read-only and closed-world, so MCP clients such as ChatGPT do not need to ask for
confirmation. Write tools are not, so the client asks the user to confirm each call.

| Tool | Purpose |
|---|---|
| `list_projects` | The allow-listed projects and the branch prefix on which changes are allowed |
| `get_tree` | List files and directories (100 per page) |
| `get_file` | Read one text file; binary and oversized files are not returned |
| `get_mr_diff` | Merge request diffs, cut to a size limit |
| `list_pipelines`, `list_pipeline_jobs` | CI pipelines and their jobs |
| `get_job_trace` | The end of a job log, cleaned of terminal escape codes |
| `create_branch` *(write)* | A new branch under the agent prefix |
| `commit_files` *(write)* | One commit of text file changes (create, update, delete) on an agent branch |
| `create_merge_request` *(write)* | A draft merge request from an agent branch into the default branch |
| `add_mr_note` *(write)* | A comment on an open merge request that comes from an agent branch |

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
`-version` prints the version.

## Deploy

The supported way is the container image built from the [`Dockerfile`](Dockerfile): OpenAI's `tunnel-client-runtime`
(pinned by version and digest, provenance verified) plus this server. Step by step in
[`docs/deployment.md`](docs/deployment.md); an example compose file is in [`deploy/`](deploy/). Then connect ChatGPT
([`docs/chatgpt-connector.md`](docs/chatgpt-connector.md)) and harden GitLab first
([`docs/gitlab-hardening.md`](docs/gitlab-hardening.md)).

## Configuration

See [`config.example.json`](config.example.json). The configuration never contains the token itself, only where to
read it (`token_file`, preferred, or `token_env`).

| Field | Meaning |
|---|---|
| `base_url` | GitLab instance root. `https` only unless `allow_insecure_http` is set |
| `ca_file` | Optional absolute path to a PEM bundle for a private CA |
| `token_file` / `token_env` | Exactly one; where to read the GitLab token |
| `projects` | Allow-list of `{id, path}`. Requests are sent by numeric ID, never by user-supplied path |
| `enable_write_tools` | Register the four write tools (default `false`, so the server is read-only) |
| `branch_prefix` | Branches the agent may write to (default `agent/`) |
| `write_deny_paths` | Paths the agent may never write (default `.gitlab-ci.yml`, `.gitlab`, `CODEOWNERS`) |
| `request_timeout_seconds`, `limits` | Timeouts and size caps (files, commits, responses, diffs, logs), with hard ceilings |

Unknown fields are rejected.

## GitLab-side setup (required)

The in-server checks are defence in depth. The real boundary is GitLab itself: use a project access token with the
Developer role (`read_api` scope is enough while write tools are off, `api` is needed to write) and a short expiry,
protect the default branch against direct pushes and merges by the bot, require human approval and CODEOWNERS review,
and keep CI secrets protected and masked. Agent-written code runs in CI before anyone reviews it, so read the full
checklist in [`docs/gitlab-hardening.md`](docs/gitlab-hardening.md). The merge request diff tool uses the `/diffs`
endpoint, which needs GitLab 15.7 or newer.

## Development

```sh
go vet ./...
go test -race ./...
go test ./internal/validate -fuzz FuzzFilePath -fuzztime 30s   # also FuzzBranchName, FuzzProjectPath, FuzzText, FuzzResolve
```

Project context, design decisions and the task plan live in [`.claude/`](.claude/). Security reports: see
[`SECURITY.md`](SECURITY.md). Licensed under the Apache License 2.0 (see [`LICENSE`](LICENSE)).
