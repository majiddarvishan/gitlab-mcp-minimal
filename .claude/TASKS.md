# TASKS

Marking: `[x]` means done and verified, `[ ]` means open. A parent task is checked only when all its sub-items are.
Update a task's status in the same PR that changes it.

Status: **Phases 0 to 2 implemented. Next: Phase 3 (write tools).**

## Phase 0: Repository setup

- [ ] **T0.1** README, LICENSE (see D12), `.gitignore`, `go.mod`, `SECURITY.md`.
  - [x] README, `.gitignore`, `go.mod`, `SECURITY.md`, `config.example.json` (parsed by a test so it cannot drift)
  - [ ] LICENSE: waiting for the maintainer's decision (Apache-2.0 or MIT)
- [ ] **T0.2** CI: `go vet`, `gofmt` check, `go test -race`, `govulncheck`, `gitleaks`; actions pinned by commit SHA;
  Dependabot.
  - [x] Dependabot for Go modules and GitHub Actions
  - [x] `.github/workflows/ci.yml` (committed by the maintainer; the connector cannot write workflow files)
  - [ ] First green CI run on GitHub (needs `go.sum`, see the note under Phase 2)
- [ ] **T0.3** (maintainer) protect `main` on GitHub (PR required, CI required) and enable private vulnerability
  reporting.

## Phase 1: Core logic, no network

Verified locally with Go 1.26: `go vet`, `gofmt`, `go test -race`, 8 s of fuzzing per fuzz target, and mutation
checks (deliberately breaking a rule and confirming a test fails).

- [x] **T1.2** validators (needs `internal/secret`; done before T1.1 because config depends on it).
- [x] **T1.1** config loader and validation.
- [x] **T1.3** audit logger.
- [x] **T1.4** GitLab client (needs T1.1, T1.2).

## Phase 2: Read tools

Implemented on the official Go SDK (D10) and verified locally: `go vet`, `gofmt`, `go test -race` for every package,
an end-to-end test that builds the real binary and drives it over stdio with the SDK's own client, and 16 mutation
checks on the tool code (all caught by a test).

**Needs the maintainer:** `go.sum` is not committed, because it must be generated against the Go checksum database,
which the assistant's sandbox cannot reach. Run `go mod tidy` on this branch, review the `go.sum` diff and commit it.
Until then CI fails with missing `go.sum` entries.

- [x] **T2.1** MCP server skeleton over stdio and `list_projects`; SDK decision recorded (D10).
  Server starts, lists its tools, returns only allow-listed projects; logs go to stderr; tests cover tool
  annotations, schemas, untrusted-content instructions, audit lines, and startup failures leaving stdout empty.
- [x] **T2.2** `get_tree` and `get_file`.
  Size cap, binary files not returned (invalid UTF-8 or NUL bytes), ref and path validation, default branch looked up
  once and cached, path escaped as a single URL segment.
- [x] **T2.3** `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`, `get_job_trace`.
  Diff size budget cut at character boundaries, job logs streamed and tail-truncated with escape codes removed, enum
  and ID validation, no links in output.
- [ ] **T2.4** (optional, needs a decision in D5) `get_merge_request` and `list_mr_notes`, so an agent can read a
  merge request's description and review comments.

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
  (hostile issue text, hostile file content, hostile CI log). Confirm no write lands outside the prefix, that CI
  secrets are not exposed on agent branches, and that the read tools' field names match a real GitLab.

## Phase 5: Hardening and release

- [ ] **T5.1** security review pass (threat model against implementation, fuzz run, dependency review,
  `govulncheck` clean).
- [ ] **T5.2** make-public checklist (`gitleaks` over the full history, scrub identifiers, LICENSE,
  SECURITY.md, README risk section, support statement).
- [ ] **T5.3** (optional) tagged release with checksums.

## Open questions for the maintainer

- Licence: Apache-2.0 or MIT (D12).
- GitLab version in use: the diff tool needs 15.7 or newer, and `draft` handling for merge requests depends on it.
- Whether to add T2.4 before the write tools.
- Whether the default limits (20 files per commit, 256 KiB per file, 1 MiB per commit, 128 KiB of diff, 64 KiB of job
  log) suit your repositories.
