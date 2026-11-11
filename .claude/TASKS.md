# TASKS

Status: **DRAFT, pending maintainer review.**
Task statuses: `todo`, `in-progress`, `done`, `blocked`. Update a task's status in the same PR that changes it.

## Phase 0: Repository setup

- **T0.1** `todo`: README, LICENSE (see D12), `.gitignore`, `go.mod`, `SECURITY.md`.
  Accept: the empty module builds; README states scope, non-goals and risks.
- **T0.2** `todo`: GitHub Actions CI with `go vet`, `go test -race`, `govulncheck` and `gitleaks`; actions pinned by
  commit SHA; Dependabot for Go modules and actions.
- **T0.3** `todo` (maintainer): protect `main` on GitHub (PR required, CI required).

## Phase 1: Core logic, no network

- **T1.1** `todo`: config loader and validation.
  Accept: unknown fields rejected; token read from file or environment and never printed; allow-list, branch
  prefix, limits and audit settings validated with clear errors; a test for each failure mode.
- **T1.2** `todo` (needs T1.1): validators.
  Accept: project allow-list matching (numeric ID and normalised path); branch names (prefix policy, character
  set, length, no leading `-`, no `..`, no control characters); file paths (relative, normalised, no `..`, no NUL,
  length cap); refs; numeric IDs. Table-driven and fuzz tests; documented rejection reasons.
- **T1.3** `todo`: audit logger.
  Accept: JSON lines on stderr only; token redacted; no file contents; a test proving stdout stays untouched.
- **T1.4** `todo` (needs T1.1, T1.2): GitLab client.
  Accept: fixed base URL; `PRIVATE-TOKEN` header; timeouts; response size caps; no cross-host redirects; path
  segments escaped; `httptest` tests including redirect and oversize cases.

## Phase 2: Read tools

- **T2.1** `todo` (needs Phase 1): MCP server skeleton over stdio and `list_projects`; record the SDK decision (D10).
  Accept: the server starts, lists its tools and returns only allow-listed projects; logs go to stderr; an
  integration test with an in-process client.
- **T2.2** `todo`: `get_tree` and `get_file`.
  Accept: size cap; a binary-file policy (reject, or base64 with a cap) written down and tested; ref validation.
- **T2.3** `todo`: `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`, `get_job_trace`.
  Accept: job traces tail-truncated; diff size capped.

## Phase 3: Write tools

- **T3.1** `todo`: `create_branch`.
  Accept: the name must match the configured prefix; the source ref is validated; default and protected names are
  refused.
- **T3.2** `todo`: `commit_files` (create, update, delete actions).
  Accept: limits enforced; the target branch must match the prefix; the default and protected branches are refused;
  a single atomic commit; commit message length cap.
- **T3.3** `todo`: `create_merge_request` and `add_mr_note`.
  Accept: MRs are always drafts; the source branch must match the prefix; the target is restricted; note length cap.
- **T3.4** `todo`: negative test suite.
  Accept: writes to the default branch, path traversal, projects outside the allow-list, oversized commits,
  cross-host redirects and control characters are all rejected, and tests assert that no outbound request was made.

## Phase 4: Packaging and integration

- **T4.1** `todo`: Dockerfile with both binaries; non-root, read-only root filesystem; `tunnel-client` pinned to a
  version and checksum-verified.
- **T4.2** `todo`: `tunnel-client` profile and setup documentation. VERIFY every flag against OpenAI's guide.
- **T4.3** `todo`: ChatGPT connector setup guide (Developer mode, Tunnel connection, authentication choice on the
  connector, who can use it).
- **T4.4** `todo`: GitLab hardening checklist document derived from DECISIONS.md.
- **T4.5** `todo`: end-to-end test on a disposable GitLab project: the happy path plus prompt-injection scenarios
  (hostile issue text, hostile file content, hostile CI log). Confirm no write lands outside the prefix and that CI
  secrets are not exposed on agent branches.

## Phase 5: Hardening and release

- **T5.1** `todo`: security review pass (threat model against implementation, fuzz run, dependency review,
  `govulncheck` clean).
- **T5.2** `todo`: make-public checklist (`gitleaks` over the full history, scrub identifiers, LICENSE,
  SECURITY.md, README risk section, support statement).
- **T5.3** `todo` (optional): tagged release with checksums.

## Open questions for the maintainer

- Licence: Apache-2.0 or MIT (D12).
- MCP SDK: confirm the official Go SDK as the default (D10).
- Default values for limits (files per commit, bytes per file, trace size).
- GitLab version in use, to settle the diff endpoint and `draft` handling.
