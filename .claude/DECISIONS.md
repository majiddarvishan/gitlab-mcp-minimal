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

## D5. Tool set (accepted and implemented)

- Read: `list_projects`, `get_tree`, `get_file`, `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`,
  `get_job_trace`.
- Write (only with `enable_write_tools`, see D17): `create_branch`, `commit_files`, `create_merge_request`,
  `add_mr_note`.
- Candidates that need their own decision before being added: `get_merge_request` (title, state, branches) and
  `list_mr_notes`, which an agent needs to react to review comments.
- Explicitly absent: merging MRs, deleting branches or projects, CI variables, raw API calls, GraphQL, uploads, and
  anything that touches the local filesystem.

## D6. Write policy (accepted and implemented)

- Writes only to branches matching a configured prefix (default `agent/`). The branch must exist, and
  `commit_files` looks it up first and refuses the default branch and protected branches.
- Writes to paths on the deny-list are refused (see D14). Content must be UTF-8 text without NUL bytes; there is no
  way to write binary files.
- Merge requests are always created as drafts (a `Draft:` title prefix, which GitLab recognises). They come from an
  agent branch and always target the project's default branch.
- Comments (`add_mr_note`) are only allowed on open merge requests whose source branch is an agent branch.
- A commit may carry per-file `last_commit_id` values so that GitLab refuses it if a file changed meanwhile.

## D7. Limits (accepted)

Maximum files per commit, bytes per file, total bytes per commit, response size, job log size, merge request diff
size and free-text size, plus request timeouts. Values live in configuration with conservative defaults and hard
ceilings that a configuration cannot exceed. Implemented in `internal/config`.

## D8. Configuration (accepted)

JSON file decoded with unknown fields and trailing data rejected, size-capped at 64 KiB. Fields: `base_url`,
`allow_insecure_http`, `ca_file`, `token_file` or `token_env` (exactly one), `projects`, `enable_write_tools`,
`branch_prefix`, `write_deny_paths`, `request_timeout_seconds`, `limits`. The token is read from a file (preferred;
symlinks such as Kubernetes secret mounts are followed) or an environment variable, never from tool arguments, and is
wrapped in a type that never prints (`internal/secret`).

## D9. Logging (accepted)

Structured JSON audit log, one line per tool call (tool, project ID, ref, outcome, short reason, duration; no file
contents), written to stderr only. The logger refuses to be constructed on stdout, scrubs known secrets and bounds
field lengths. Known gap: calls rejected by the SDK's schema validation never reach the handlers and are not logged.

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

## D11. Packaging (accepted, see D18)

One container image, non-root (uid 65532), read-only root filesystem and no capabilities in the example deployment, no
shell of our own, the OpenAI runtime key and the GitLab token mounted as secret files, the `tunnel-client-runtime`
base image pinned by exact version and digest, and egress limited to the OpenAI tunnel host and the GitLab host.

## D12. Licence (accepted)

Apache-2.0, added to the repository by the maintainer.

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

- Every tool is annotated with MCP hints. Read tools are `readOnlyHint: true`, so clients such as ChatGPT treat them
  as safe. Write tools carry no `readOnlyHint`, so clients ask the user to confirm each call; `commit_files` is also
  flagged `destructiveHint: true` because an update or delete replaces existing content. All tools are
  `openWorldHint: false`.
- Outputs never contain links or the address of the GitLab instance, and only the fields a tool needs are returned.
- Errors shown to the model are short and come from a fixed set (`internal/server/errors.go`); anything unexpected is
  reported as "internal error" and the details go to the audit log only.
- Binary files are never returned; text files above the limit are refused rather than truncated; diffs and job logs
  are cut to their limits with a `truncated` flag; job logs have terminal escape codes, section markers and control
  characters removed.
- The server instructions tell the model that all returned content is untrusted data and must not be obeyed.
- Malformed input is rejected before any request is made to GitLab, and each rejection is audited as `denied`.

## D17. Write tools are opt-in (accepted)

The four write tools are registered only when the configuration sets `enable_write_tools: true`. Without it the
server exposes the seven read tools only, so it can be deployed read-only first, and a token with `read_api` scope
is enough. This is a restriction, not a new capability.

## D18. Deployment shape (accepted)

Two programs in one image: OpenAI's `tunnel-client-runtime` is the entry point and starts this server as a supervised
stdio child (`MCP_COMMAND`). Reasons and consequences, from the upstream documentation:

- The runtime flavour is the narrow one (commands `run`, help, version; no admin UI, plugins or local proxy). It is
  published as `ghcr.io/openai/tunnel-client-runtime` with an SBOM and signed provenance, so the base image is one
  pinned, verifiable artifact instead of a downloaded binary. The Dockerfile has no default for it, so an unpinned
  build fails.
- Both secrets are files, not environment variables: the OpenAI runtime key through `--control-plane.api-key=file:`
  and the GitLab token through `token_file`. The stdio child inherits the runtime's environment, so this keeps both
  secrets out of it.
- Exactly one instance per tunnel id (upstream limit for stdio servers, including during restarts and rollouts).
- Egress: `api.openai.com:443` and the GitLab host only; no inbound ports.

Alternatives considered and rejected: embedding the tunnel client in this process through its Go SDK (puts OpenAI's
network client and the GitLab token in one process and couples release cadences), an HTTP MCP endpoint (adds a
listener, D2), and release ZIP plus systemd (valid, but the image gives one pinned, attested artifact).

## Threat model (summary)

Assets: the GitLab token, the source code of allow-listed projects, and the integrity of branches and CI.

| Threat | Mitigation |
|---|---|
| Prompt injection through issue or MR text, file content or job logs steering the model into harmful tool calls | Narrow tool set; writes only to prefixed branches and draft MRs; no merge tool; deny-listed paths; ChatGPT asks for confirmation before write actions; a human reviews every MR; server instructions mark content as untrusted |
| Malicious or buggy tool arguments (path traversal, odd refs, IDs) | Strict validators with fuzz and negative tests; URL path segments always escaped; numeric IDs parsed, never concatenated |
| Redirecting the API base URL to steal the token (SSRF) | Fixed base URL, no per-request URL, no redirects, no proxy |
| Local file read or exfiltration through the server | No tools touching local files; non-root, read-only filesystem, no other secrets on the host |
| Pipeline changes on agent branches that exfiltrate CI secrets | Deny-list for CI paths (D14); GitLab side: protected variables, protected runners where needed, CODEOWNERS on the CI configuration |
| Agent-written code (tests, scripts, build files) runs in CI on its branch before anyone reviews it | GitLab side only (hardening checklist, section 6): protected and masked variables, deploy jobs on protected refs only, credential-free runners for unprotected branches |
| Overwriting a human's changes | Agent branches only; `last_commit_id` support; protected and default branches refused; a human reviews and merges |
| Token leakage through logs or errors | Redacting secret type; audit logger scrubs secrets; never log headers, bodies or file contents; secrets only from files; neither secret in the child's environment |
| Sensitive data in job logs or files reaching the model provider | Accepted residual risk: everything a tool returns is sent to the model provider. Keep secrets masked in CI and out of repositories; restrict the allow-list |
| Supply chain (dependencies, tunnel-client image, base images) | Few dependencies, pinned versions, `govulncheck`, CI actions pinned by SHA, base image pinned by digest and its provenance verified |
| Unauthenticated network access to the server | No listener at all (stdio only); no inbound ports on the container |

## GitLab-side requirements (not enforced by this server)

The full checklist is `docs/gitlab-hardening.md`. In short:

- A project access token per project: Developer role, `read_api` scope (read-only) or `api` (write), short expiry.
- Default branch protected: no direct pushes, merges by Maintainers only; agent branches not protected.
- Approval and CODEOWNERS rules so a person reviews every change, including CI configuration and scripts.
- CI secrets protected and masked; deploy jobs only on protected refs; credential-free runners for other branches.
- Monitor GitLab audit events for the bot's activity.

## GitLab REST mapping (VERIFY against the target instance's API documentation and version)

| Tool | Endpoint |
|---|---|
| `get_tree` | `GET /projects/:id/repository/tree` |
| `get_file` | `GET /projects/:id/repository/files/:file_path?ref=` (the default branch comes from `GET /projects/:id`) |
| `get_mr_diff` | `GET /projects/:id/merge_requests/:iid/diffs` (GitLab 15.7 or newer) |
| `list_pipelines` | `GET /projects/:id/pipelines` |
| `list_pipeline_jobs` | `GET /projects/:id/pipelines/:pipeline_id/jobs` |
| `get_job_trace` | `GET /projects/:id/jobs/:job_id/trace` |
| `create_branch` | `POST /projects/:id/repository/branches` (JSON body: `branch`, `ref`) |
| `commit_files` | `GET /projects/:id/repository/branches/:branch` (policy check), then `POST /projects/:id/repository/commits` (`branch`, `commit_message`, `actions[]`) |
| `create_merge_request` | `POST /projects/:id/merge_requests` (`source_branch`, `target_branch`, `title` with a `Draft:` prefix, `description`) |
| `add_mr_note` | `GET /projects/:id/merge_requests/:iid` (policy check), then `POST /projects/:id/merge_requests/:iid/notes` |

All endpoints are exercised only against a fake GitLab in tests, so their field names and the `Draft:` behaviour are
VERIFY items until the end-to-end test (T4.5) runs against a real instance.
