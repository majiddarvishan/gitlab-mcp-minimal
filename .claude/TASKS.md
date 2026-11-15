# TASKS

Marking: `[x]` means done and verified, `[ ]` means open. A parent task is checked only when all its sub-items are.
Update a task's status in the same PR that changes it.

Status: **Phases 0 to 3 implemented. Next: Phase 4 (packaging and end-to-end test against a real GitLab).**

Target instance: self-managed GitLab 18.8 (Enterprise Edition). Both version-dependent features are available there:
the merge request `/diffs` endpoint (since 15.7) and `Draft:` title prefixes, which GitLab's documentation names as
the way to create a draft merge request (the create API has no draft parameter).

## Phase 0: Repository setup

- [x] **T0.1** README, LICENSE (Apache-2.0, added by the maintainer), `.gitignore`, `go.mod`, `SECURITY.md`.
- [x] **T0.2** CI: `go vet`, `gofmt` check, `go test -race`, `govulncheck`, `gitleaks`; actions pinned by commit SHA;
  Dependabot. First green run confirmed on GitHub (jobs `test` and `gitleaks`).
- [ ] **T0.3** (maintainer) protect `main` on GitHub (PR required, CI required) and enable private vulnerability
  reporting.

## Phase 1: Core logic, no network

- [x] **T1.2** validators (needs `internal/secret`; done before T1.1 because config depends on it).
- [x] **T1.1** config loader and validation.
- [x] **T1.3** audit logger.
- [x] **T1.4** GitLab client (needs T1.1, T1.2).

## Phase 2: Read tools

- [x] **T2.1** MCP server skeleton over stdio and `list_projects`; SDK decision recorded (D10).
- [x] **T2.2** `get_tree` and `get_file`.
- [x] **T2.3** `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs`, `get_job_trace`.
- [ ] **T2.4** (optional, needs a decision in D5) `get_merge_request` and `list_mr_notes`, so an agent can read a
  merge request's description and review comments.

## Phase 3: Write tools

Verified locally with Go 1.26 against the real SDK: `go vet`, `gofmt`, `go test -race` for every package, and 28
mutation checks on the write policy (every deliberate breakage made a test fail). Write tools are registered only when
the configuration sets `enable_write_tools` (D17).

- [x] **T3.1** `create_branch`.
  The name must match the configured prefix; the start ref is validated; refused input never reaches GitLab.
- [x] **T3.2** `commit_files` (create, update, delete actions).
  Prefix, deny-list and size limits enforced; text only (UTF-8, no NUL); duplicate paths refused; optional
  `last_commit_id` per file to detect concurrent changes; the branch is looked up first and a default or protected
  branch is refused; one atomic commit.
- [x] **T3.3** `create_merge_request` and `add_mr_note`.
  Merge requests are always drafts (title prefix), come from an agent branch and target the default branch; notes are
  only allowed on open merge requests from agent branches.
- [x] **T3.4** negative test suite.
  Writes to the default branch, protected branches, foreign branches, deny-listed paths, traversal, oversized commits,
  binary content, unknown projects and invalid titles are all rejected, and tests assert that no request reached
  GitLab.

Known gap: a call that the SDK rejects during schema validation (for example a missing required field) never reaches
the handlers and therefore is not written to the audit log. It has no effect on GitLab.

## Phase 4: Packaging and integration

- [ ] **T4.1** Dockerfile with both binaries; non-root, read-only root filesystem; `tunnel-client` pinned to a
  version and checksum-verified.
- [ ] **T4.2** `tunnel-client` profile and setup documentation. VERIFY every flag against OpenAI's guide.
- [ ] **T4.3** ChatGPT connector setup guide (Developer mode, Tunnel connection, authentication choice on the
  connector, who can use it).
- [ ] **T4.4** GitLab hardening checklist document derived from DECISIONS.md.
- [ ] **T4.5** end-to-end test on a disposable GitLab project (GitLab 18.8): the happy path plus prompt-injection
  scenarios (hostile issue text, hostile file content, hostile CI log). Confirm no write lands outside the prefix, that
  CI secrets are not exposed on agent branches, and that the field names of the GitLab responses (all tools) match a
  real instance.

## Phase 5: Hardening and release

- [ ] **T5.1** security review pass (threat model against implementation, fuzz run, dependency review,
  `govulncheck` clean). Candidates: audit calls rejected by schema validation, a cap on write calls per hour.
- [ ] **T5.2** make-public checklist (`gitleaks` over the full history, scrub identifiers, LICENSE,
  SECURITY.md, README risk section, support statement).
- [ ] **T5.3** (optional) tagged release with checksums.

## Open questions for the maintainer

- Whether to add T2.4 (read merge request details and review comments) before the end-to-end test.
- Whether the default limits (20 files per commit, 256 KiB per file, 1 MiB per commit, 128 KiB of diff, 64 KiB of job
  log) suit your repositories.
- Whether `.gitmodules` should join the default write deny-list (a changed submodule URL is a supply-chain risk,
  although a human reviews the merge request).
