# TASKS

Marking: `[x]` means done and verified, `[ ]` means open. A parent task is checked only when all its sub-items are.
Update a task's status in the same PR that changes it.

Status: **DRAFT plan, Phase 0 and Phase 1 implemented, pending maintainer review.**

## Phase 0: Repository setup

- [ ] **T0.1** README, LICENSE (see D12), `.gitignore`, `go.mod`, `SECURITY.md`.
  - [x] README, `.gitignore`, `go.mod`, `SECURITY.md`, `config.example.json` (parsed by a test so it cannot drift)
  - [ ] LICENSE: waiting for the maintainer's decision (Apache-2.0 or MIT)
- [ ] **T0.2** CI: `go vet`, `gofmt` check, `go test -race`, `govulncheck`, `gitleaks`; actions pinned by commit SHA;
  Dependabot.
  - [x] Dependabot for Go modules and GitHub Actions
  - [ ] Add `.github/workflows/ci.yml` (maintainer: the connector cannot write workflow files; the file was
    prepared, linted with actionlint, and its gitleaks step was run locally)
  - [ ] First green CI run on GitHub
- [ ] **T0.3** (maintainer) protect `main` on GitHub (PR required, CI required) and enable private vulnerability
  reporting.

## Phase 1: Core logic, no network

Verified locally with Go 1.26: `go vet`, `gofmt`, `go test -race`, 8 s of fuzzing per fuzz target, and 11 mutation
checks (deliberately breaking a rule and confirming a test fails).

- [x] **T1.2** validators (needs `internal/secret`; done before T1.1 because config depends on it).
  Project paths, branch names and prefix policy, file paths, write deny-list (D14), refs, IDs, free text; allow-list
  with case-insensitive path matching and numeric-ID resolution; table-driven and fuzz tests; bounded error messages.
- [x] **T1.1** config loader and validation.
  Strict JSON, defaults and hard ceilings for limits, token from file or environment with validation, token never in
  errors or formatted output.
- [x] **T1.3** audit logger.
  JSON lines on stderr only, refuses stdout, secret scrubbing, bounded fields, tested against log injection.
- [x] **T1.4** GitLab client (needs T1.1, T1.2).
  Fixed base URL, `PRIVATE-TOKEN` header, timeouts, response size caps, no redirects, no proxy, escaped path
  segments, TLS with optional private CA; `httptest` tests including redirect, oversize and traversal cases.

## Phase 2: Read tools

- [ ] **T2.1** MCP server skeleton over stdio and `list_projects`; record the SDK decision (D10). Needs Phase 1.
  Accept: the server starts, lists its tools and returns only allow-listed projects; logs go to stderr; an
  integration test with an in-process client.
- [ ] **T2.2** `get_tree` and `get_file`.
  Accept: size cap; a binary-file policy (reject, or base64 with a cap) written down and tested; ref validation.
- [ ] **T2.3** `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`, `get_job_trace`.
  Accept: job traces tail-truncated; diff size capped.

## Phase 3: Write tools

- [ ] **T3.1** `create_branch`.
  Accept: the name must match the configured prefix; the source ref is validated; default and protected names are
  refused.
- [ ] **T3.2** `commit_files` (create, update, delete actions).
  Accept: limits and deny-list enforced; the target branch must match the prefix; the default and protected
  branches are refused; a single atomic commit; commit message length cap.
- [ ] **T3.3** `create_merge_request` and `add_mr_note`.
  Accept: MRs are always drafts; the source branch must match the prefix; the target is restricted; note length cap.
- [ ] **T3.4** negative test suite.
  Accept: writes to the default branch, path traversal, deny-listed paths, projects outside the allow-list,
  oversized commits, cross-host redirects and control characters are all rejected, and tests assert that no outbound
  request was made.

## Phase 4: Packaging and integration

- [ ] **T4.1** Dockerfile with both binaries; non-root, read-only root filesystem; `tunnel-client` pinned to a
  version and checksum-verified.
- [ ] **T4.2** `tunnel-client` profile and setup documentation. VERIFY every flag against OpenAI's guide.
- [ ] **T4.3** ChatGPT connector setup guide (Developer mode, Tunnel connection, authentication choice on the
  connector, who can use it).
- [ ] **T4.4** GitLab hardening checklist document derived from DECISIONS.md.
- [ ] **T4.5** end-to-end test on a disposable GitLab project: the happy path plus prompt-injection scenarios
  (hostile issue text, hostile file content, hostile CI log). Confirm no write lands outside the prefix and that CI
  secrets are not exposed on agent branches.

## Phase 5: Hardening and release

- [ ] **T5.1** security review pass (threat model against implementation, fuzz run, dependency review,
  `govulncheck` clean).
- [ ] **T5.2** make-public checklist (`gitleaks` over the full history, scrub identifiers, LICENSE,
  SECURITY.md, README risk section, support statement).
- [ ] **T5.3** (optional) tagged release with checksums.

## Open questions for the maintainer

- Licence: Apache-2.0 or MIT (D12).
- MCP SDK: confirm the official Go SDK as the default (D10).
- GitLab version in use, to settle the diff endpoint and `draft` handling.
- Whether the default limits (20 files per commit, 256 KiB per file, 1 MiB per commit, 64 KiB of job trace) suit
  your repositories.
