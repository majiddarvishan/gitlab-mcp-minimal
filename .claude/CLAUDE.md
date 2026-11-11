# CLAUDE.md: gitlab-mcp-minimal

Project context and working rules for AI-assisted sessions on this repository.
Read this file, `DECISIONS.md` and `TASKS.md` before starting any work.

## Goal

A minimal [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server in Go that lets an AI assistant
(ChatGPT desktop/web through a Developer-mode custom connector) **read repositories and propose changes on a
private, self-hosted GitLab instance through branches and merge requests, and nothing else.**

Deliberate non-goals: general GitLab API coverage, GraphQL or raw-API passthrough, file upload, any local
filesystem access, HTTP/SSE transports, merging MRs, writing to default or protected branches.

## Deployment shape

```
ChatGPT -> OpenAI-hosted tunnel endpoint <- outbound HTTPS <- tunnel-client (inside the private network)
                                                              |
                                          this server (stdio child process) -> GitLab REST API
```

- One container, two binaries. `tunnel-client` (OpenAI, open source, `github.com/openai/tunnel-client`) starts this
  server as a child process over stdio. Exact flags and profile format: VERIFY against OpenAI's "Secure MCP Tunnel"
  guide.
- stdout is the MCP protocol channel. **Never write logs to stdout.** Logs go to stderr.
- The GitLab instance is private. Its hostname, project IDs/paths, tokens and any organisation-specific names must
  never appear in this repository (code, tests, docs, examples, commit messages). Use `gitlab.example.com` and fake
  IDs.

## Conventions

- Conversation with the maintainer happens in Persian. Code, comments, commit messages, docs, issues and PRs are
  written in English.
- Go, standard library first. Every third-party dependency needs a written justification in its PR. Pin versions.
- Module path: `github.com/majiddarvishan/gitlab-mcp-minimal`.

## Working rules

1. Never push to `main`. Work on `claude/<topic>` branches and open a PR for the maintainer to review and merge.
2. One task (or sub-task) per PR, small diffs. Put the task ID (for example `T1.2`) in the PR title and update the
   task's status in `TASKS.md`.
3. The assistant's sandbox cannot download Go modules, so it usually cannot compile code or run tests. CI is the
   source of truth. Keep code simple, check external APIs against primary documentation before using them, and list
   what is unverified in the PR description.
4. Tests first for validators and policy checks (table-driven and fuzz). Every security-relevant rule needs a
   negative test.
5. No secrets, ever. Never log tokens, request headers or file contents.
6. Mark unverified facts (library APIs, GitLab endpoints and versions, tunnel-client flags) with `VERIFY` until they
   are checked against the primary source.
7. No scope creep. A new tool or capability needs an entry in `DECISIONS.md` first.
