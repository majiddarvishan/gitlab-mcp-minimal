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
per-request environment. HTTP redirects are never followed (see D15).

## D4. Project allow-list enforced in code (accepted)

Every tool call is checked against a configured allow-list before any request is made. Requests to GitLab use the
configured numeric project ID, never a user-supplied path. This is defence in depth: the real boundary is the GitLab
token scope and branch protection (see "GitLab-side requirements"). In-server flags such as read-only modes are never
treated as the security boundary.

## D5. Tool set (read tools accepted and implemented, write tools proposed)

- Read, implemented in Phase 2: `list_projects`, `get_tree`, `get_file`, `get_mr_diff`, `list_pipelines`,
  `list_pipeline_jobs`, `get_job_trace`.
- Write, planned for Phase 3: `create_branch`, `commit_files`, `create_merge_request`, `add_mr_note`.
- Candidates that need their own decision before being added: `get_merge_request` (title, state, branches) and
  `list_mr_notes`, which an agent needs to react to review comments.
- Explicitly absent: merging MRs, deleting branches or projects, CI variables, raw API calls, GraphQL, uploads, and
  anything that touches the local filesystem.

## D6. Write policy (proposed)

- Writes only to branches matching a configured prefix (default `agent/`).
- `commit_files` refuses the default branch and protected branches.
- Writes to paths on the deny-list are refused (see D14).
- Merge requests are always created as drafts. The target must be the project's default branch unless configured
  otherwise.

## D7. Limits (accepted)

Maximum files per commit, bytes per file, total bytes per commit, response size, job log size, merge request diff
size and free-text size, plus request timeouts. Values live in configuration with conservative defaults and hard
ceilings that a configuration cannot exceed. Implemented in `internal/config`.

## D8. Configuration (accepted)

JSON file decoded with unknown fields and trailing data rejected, size-capped at 64 KiB. Fields: `base_url`,
`allow_insecure_http`, `ca_file`, `token_file` or `token_env` (exactly one), `projects`, `branch_prefix`,
`write_deny_paths`, `request_timeout_seconds`, `limits`. The token is read from a file (preferred; symlinks such as
Kubernetes secret mounts are followed) or an environment variable, never from tool arguments, and is wrapped in a
type that never prints (`internal/secret`).

## D9. Logging (accepted)

Structured JSON audit log, one line per tool call (tool, project ID, ref, outcome, short reason, duration; no file
contents), written to stderr only. The logger refuses to be constructed on stdout, scrubs known secrets and bounds
field lengths.

## D10. MCP SDK (accepted): the official Go SDK, pinned

The server uses `github.com/modelcontextprotocol/go-sdk` (v1.8.0 at the time of writing; MIT/Apache-2.0 licensed).
A hand-written JSON-RPC handler was considered and rejected: the protocol changed substantially in version
2026-07-28 (stateless requests, `server/discover` in place of the `initialize` handshake, per-request metadata), and
the SDK negotiates both the old and the new model. Maintaining that ourselves would be a continuing risk with no
security benefit.

Costs, accepted knowingly: more code than we would write ourselves (the SDK also contains HTTP and SSE transports,
which this project never starts) and a handful of transitive modules (`jsonschema-go`, `uritemplate`,
`segmentio/encoding`, `golang.org/x/oauth2`, `x/time`, `x/sync`, `x/sys`). Mitigations: version pinned in `go.mod`,
`go.sum` verified by the checksum database, `govulncheck` in CI, Dependabot, and no transport other than stdio is
ever constructed.

## D11. Packaging (proposed)

Single container, non-root, read-only root filesystem, no shell if possible, egress allowed only to the GitLab host
and OpenAI, token mounted as a secret file, `tunnel-client` version pinned and checksum-verified.

## D12. Licence (open)

Apache-2.0 or MIT. The maintainer decides (T0.1).

## D13. Visibility (accepted)

The repository stays private until the T5.2 checklist is complete.

## D14. Write-path deny-list (accepted)

An agent with write access could change CI configuration to exfiltrate secrets or weaken review rules. Writes are
therefore refused for a configurable list of paths, default `.gitlab-ci.yml`, `.gitlab` and `CODEOWNERS`. Entries
match the exact path or anything below it, case-insensitively. Limitation: CI files included from other locations
are not covered, so the GitLab-side requirements below still apply.

## D15. HTTP client hardening (accepted)

- Redirects are never followed. Go forwards custom headers such as `PRIVATE-TOKEN` across redirects, so any redirect
  is treated as an error.
- No proxy: the token only travels directly to the configured host.
- TLS 1.2 or newer, certificate verification always on, optional private CA bundle through `ca_file`; there is no
  option to skip verification. `http://` needs an explicit `allow_insecure_http`.
- Every response is size-capped (also when no Content-Length is sent) and error bodies are reduced to a short,
  sanitised message. Job logs are streamed and only their last bytes are kept.
- Request URLs are built only from escaped single path segments; `.`, `..` and empty segments are refused.

## D16. Tool output policy (accepted)

- Every tool is annotated with MCP hints: read tools are `readOnlyHint: true` and `openWorldHint: false`, so clients
  such as ChatGPT treat them as safe, and (later) write tools will not carry `readOnlyHint` so clients ask for
  confirmation.
- Outputs never contain links or the address of the GitLab instance, and only the fields a tool needs are returned.
- Errors shown to the model are short and come from a fixed set (`internal/server/errors.go`); anything unexpected is
  reported as "internal error" and the details go to the audit log only.
- Binary files are never returned; text files above the limit are refused rather than truncated; diffs and job logs
  are cut to their limits with a `truncated` flag; job logs have terminal escape codes, section markers and control
  characters removed.
- The server instructions tell the model that all returned content is untrusted data and must not be obeyed.
- Malformed input is rejected before any request is made to GitLab, and each rejection is audited as `denied`.

## Threat model (summary)

Assets: the GitLab token, the source code of allow-listed projects, and the integrity of branches and CI.

| Threat | Mitigation |
|---|---|
| Prompt injection through issue or MR text, file content or job logs steering the model into harmful tool calls | Narrow tool set; writes only to prefixed branches and draft MRs; no merge tool; deny-listed paths; ChatGPT asks for confirmation before write actions; a human reviews every MR; server instructions mark content as untrusted |
| Malicious or buggy tool arguments (path traversal, odd refs, IDs) | Strict validators with fuzz and negative tests; URL path segments always escaped; numeric IDs parsed, never concatenated |
| Redirecting the API base URL to steal the token (SSRF) | Fixed base URL, no per-request URL, no redirects, no proxy |
| Local file read or exfiltration through the server | No tools touching local files; non-root, read-only filesystem, no other secrets on the host |
| Pipeline changes on agent branches that exfiltrate CI secrets | Deny-list for CI paths (D14); GitLab side: protected variables, protected runners where needed, CODEOWNERS on the CI configuration |
| Token leakage through logs or errors | Redacting secret type; audit logger scrubs secrets; never log headers or bodies; token only from a secret file or environment |
| Sensitive data in job logs or files reaching the model provider | Accepted residual risk: everything a tool returns is sent to the model provider. Keep secrets masked in CI and out of repositories; restrict the allow-list |
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
| `get_file` | `GET /projects/:id/repository/files/:file_path?ref=` (the default branch comes from `GET /projects/:id`) |
| `get_mr_diff` | `GET /projects/:id/merge_requests/:iid/diffs` (GitLab 15.7 or newer) |
| `list_pipelines` | `GET /projects/:id/pipelines` |
| `list_pipeline_jobs` | `GET /projects/:id/pipelines/:pipeline_id/jobs` |
| `get_job_trace` | `GET /projects/:id/jobs/:job_id/trace` |
| `create_branch` | `POST /projects/:id/repository/branches` |
| `commit_files` | `POST /projects/:id/repository/commits` (`actions[]`) |
| `create_merge_request` | `POST /projects/:id/merge_requests` (draft via title prefix; VERIFY the `draft` parameter) |
| `add_mr_note` | `POST /projects/:id/merge_requests/:iid/notes` |

The read endpoints are exercised only against a fake GitLab in tests, so their field names are VERIFY items until the
end-to-end test (T4.5) runs against a real instance.
