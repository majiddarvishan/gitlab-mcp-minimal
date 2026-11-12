# gitlab-mcp-minimal

A minimal [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server in Go that lets an AI assistant
read repositories and propose changes on a self-hosted GitLab instance, through branches and merge requests, and
nothing else.

> **Status: work in progress (pre-release).** The building blocks (configuration, validators, audit log, GitLab
> client) exist; the MCP tools are not implemented yet. Do not point this at a production GitLab.

## Why another GitLab MCP server?

General-purpose GitLab MCP servers expose a large surface (GraphQL, uploads, several transports, per-request URLs),
and that surface is where past vulnerabilities in this category came from. This project takes the opposite
approach: a handful of tools, one transport, one configured instance, and every rule enforced in code.

## What it does and does not do

| Does | Does not |
|---|---|
| Read files, trees, merge request diffs, pipelines and job logs of allow-listed projects | Merge merge requests |
| Create branches under a configured prefix (default `agent/`) | Write to default or protected branches |
| Commit files to those branches, with size limits and a write deny-list | Touch CI configuration, `CODEOWNERS` or `.gitlab/` (deny-listed by default) |
| Open draft merge requests and add notes | Run GraphQL or arbitrary API calls, upload files, read local files |
| Talk to exactly one configured GitLab instance over stdio | Listen on any network port |

## Architecture

```
ChatGPT -> OpenAI-hosted tunnel endpoint <- outbound HTTPS <- tunnel-client (inside your network)
                                                              |
                                          this server (stdio child process) -> GitLab REST API
```

`tunnel-client` is OpenAI's open-source client for its Secure MCP Tunnel; it starts this server as a child process.
stdout carries the MCP protocol, so all logs (one JSON line per tool call) go to stderr.

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
| `request_timeout_seconds`, `limits` | Timeouts and size caps, with hard ceilings |

Unknown fields are rejected.

## GitLab-side setup (required)

The in-server checks are defence in depth. The real boundary is GitLab itself: use a dedicated bot user or project
access token with the Developer role, short expiry, protect the default branch against direct pushes, put CI
configuration under CODEOWNERS, and keep CI secrets as protected variables. Details in
[`.claude/DECISIONS.md`](.claude/DECISIONS.md).

## Development

```sh
go vet ./...
go test -race ./...
go test ./internal/validate -fuzz FuzzFilePath -fuzztime 30s   # also FuzzBranchName, FuzzProjectPath, FuzzText, FuzzResolve
```

Project context, design decisions and the task plan live in [`.claude/`](.claude/). Security reports: see
[`SECURITY.md`](SECURITY.md).
