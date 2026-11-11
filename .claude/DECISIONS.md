# DECISIONS

Status legend: **accepted**, **proposed** (current default, the maintainer may change it), **open**.

## D1. Write our own server instead of using a community package (accepted)

A popular community GitLab MCP server had several publicly reported 2026 advisories: an unauthenticated SSE
transport combined with an arbitrary-file-read upload tool (CVE-2026-61560), SSRF through a request header that
redirects the API base URL (CVE-2026-61559), and a GraphQL tool that bypassed read-only mode and project
allow-lists (GHSA-5648-rgj9-v224). We have not audited that project ourselves. These are reasons to prefer a much
smaller attack surface that we fully understand, not claims about its current state.

## D2. stdio only (accepted)

No HTTP or SSE listener. The only entry point is the parent process (`tunnel-client`).

## D3. Fixed GitLab base URL (accepted)

The base URL comes from configuration at startup. It is never taken from tool arguments, request headers or
per-request environment. HTTP redirects to a different host are refused.

## D4. Project allow-list enforced in code (accepted)

Every tool call is checked against a configured allow-list before any request is made. This is defence in depth:
the real boundary is the GitLab token scope and branch protection (see "GitLab-side requirements"). In-server flags
such as read-only modes are never treated as the security boundary.

## D5. v1 tool set (proposed)

- Read: `list_projects`, `get_tree`, `get_file`, `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`,
  `get_job_trace`.
- Write: `create_branch`, `commit_files`, `create_merge_request`, `add_mr_note`.
- Explicitly absent: merging MRs, deleting branches or projects, CI variables, raw API calls, GraphQL, uploads, and
  anything that touches the local filesystem.

## D6. Write policy (proposed)

- Writes only to branches matching a configured prefix (default `agent/`).
- `commit_files` refuses the default branch and protected branches.
- Merge requests are always created as drafts. The target must be the project's default branch unless configured
  otherwise.

## D7. Limits (proposed)

Maximum files per commit, bytes per file, total bytes per commit, tail-truncated job traces, response size caps and
request timeouts. Values live in configuration with conservative defaults.

## D8. Configuration (proposed)

JSON file decoded with unknown fields rejected. The token is read from a file path (preferred) or an environment
variable, never from tool arguments.

## D9. Logging (proposed)

Structured JSON audit log (tool name, project, branch, outcome; no file contents) written to stderr, with token
redaction.

## D10. MCP SDK (open)

The official Go SDK, `github.com/modelcontextprotocol/go-sdk`, is the default candidate. VERIFY the current version,
API and licence before adopting it. Fall back to a minimal JSON-RPC stdio handler only if the SDK proves unsuitable.
Decide in T2.1.

## D11. Packaging (proposed)

Single container, non-root, read-only root filesystem, no shell if possible, egress allowed only to the GitLab host
and OpenAI, token mounted as a secret file, `tunnel-client` version pinned and checksum-verified.

## D12. Licence (open)

Apache-2.0 or MIT. The maintainer decides (T0.1).

## D13. Visibility (accepted)

The repository stays private until the T5.2 checklist is complete.

## Threat model (summary)

Assets: the GitLab token, the source code of allow-listed projects, and the integrity of branches and CI.

| Threat | Mitigation |
|---|---|
| Prompt injection through issue or MR text, file content or job logs steering the model into harmful tool calls | Narrow tool set; writes only to prefixed branches and draft MRs; no merge tool; ChatGPT asks for confirmation before write actions; a human reviews every MR |
| Malicious or buggy tool arguments (path traversal, odd refs, IDs) | Strict validators with fuzz and negative tests; URL path segments always escaped; numeric IDs parsed, never concatenated |
| Redirecting the API base URL to steal the token (SSRF) | Fixed base URL, no per-request URL, no cross-host redirects |
| Local file read or exfiltration through the server | No tools touching local files; non-root, read-only filesystem, no other secrets on the host |
| Pipeline changes on agent branches that exfiltrate CI secrets | GitLab side: protected variables, protected runners where needed, CODEOWNERS on the CI configuration |
| Token leakage through logs or errors | Redaction; never log headers or bodies; token only from a secret file or environment |
| Supply chain (dependencies, tunnel-client binary, base image) | Few dependencies, pinned versions, `govulncheck`, checksum-verified binaries, CI actions pinned by SHA |
| Unauthenticated network access to the server | No listener at all (stdio only) |

## GitLab-side requirements (not enforced by this server)

- Use a dedicated bot user or a project access token: Developer role, `api` scope (writes need it; `read_api` is
  read-only), short expiry, rotated regularly.
- Default branch protected: no direct pushes, merges done by a human.
- CODEOWNERS (or equivalent approval rules) covering the CI configuration file.
- CI secrets stored as protected variables so they are not exposed to unprotected agent branches. VERIFY the
  semantics for the GitLab version in use.
- Monitor GitLab audit events for the token's activity.

## GitLab REST mapping (VERIFY against the target instance's API documentation and version)

| Tool | Endpoint |
|---|---|
| `get_tree` | `GET /projects/:id/repository/tree` |
| `get_file` | `GET /projects/:id/repository/files/:file_path?ref=` |
| `get_mr_diff` | `GET /projects/:id/merge_requests/:iid/diffs` (older instances: `/changes`) |
| `list_pipelines` | `GET /projects/:id/pipelines` |
| `list_pipeline_jobs` | `GET /projects/:id/pipelines/:pipeline_id/jobs` |
| `get_job_trace` | `GET /projects/:id/jobs/:job_id/trace` |
| `create_branch` | `POST /projects/:id/repository/branches` |
| `commit_files` | `POST /projects/:id/repository/commits` (`actions[]`) |
| `create_merge_request` | `POST /projects/:id/merge_requests` (draft via title prefix; VERIFY the `draft` parameter) |
| `add_mr_note` | `POST /projects/:id/merge_requests/:iid/notes` |
