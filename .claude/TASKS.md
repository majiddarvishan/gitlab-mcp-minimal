# TASKS

Marking: `[x]` means done and verified, `[ ]` means open. A parent task is checked only when all its sub-items are.
Update a task's status in the same PR that changes it.

Status: **Phases 0 to 3 implemented; Phase 4 documents and packaging written. Next: build the image and run the
end-to-end test (T4.1 sub-item, T4.5), which needs the maintainer.**

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
- [x] **T3.2** `commit_files` (create, update, delete actions).
- [x] **T3.3** `create_merge_request` and `add_mr_note`.
- [x] **T3.4** negative test suite.

Known gap: a call that the SDK rejects during schema validation (for example a missing required field) never reaches
the handlers and therefore is not written to the audit log. It has no effect on GitLab.

## Phase 4: Packaging and integration

Written from the upstream `tunnel-client` documentation and source (D18). I have no Docker or OpenAI access, so nothing
in this phase has been executed end to end; the unverified points are marked VERIFY in the documents.

- [ ] **T4.1** Dockerfile with both programs; non-root, read-only root filesystem in the example; `tunnel-client-runtime`
  base image pinned by version and digest.
  - [x] `Dockerfile`, `.dockerignore`, `deploy/docker-compose.example.yml` written; `hadolint` clean; the compose
    file parses; Dependabot watches Docker base images
  - [ ] (maintainer) choose a `tunnel-client-runtime` release, verify its provenance, build the image and start it
- [x] **T4.2** `docs/deployment.md`: tunnel and key setup, image verification, build, configuration, secrets as files,
  one instance per tunnel id, the initialization-guard caveat, alternatives. Flags and behaviour were read from the
  upstream documentation and source.
- [x] **T4.3** `docs/chatgpt-connector.md`: connector creation with a Tunnel connection, what "No authentication"
  means, confirmations, first prompts, troubleshooting. ChatGPT screen labels are VERIFY items.
- [x] **T4.4** `docs/gitlab-hardening.md`: token, branch protection, approvals, CODEOWNERS, CI/CD secrets and the
  risk of agent-written code running in CI before review.
- [ ] **T4.5** run `docs/e2e-test.md` on a disposable GitLab 18.8 project and report the differences (maintainer
  executes, results come back to the assistant). Also confirms the VERIFY items: GitLab field names for all tools,
  `Draft:` behaviour, whether child stderr reaches the container log, `readyz` behaviour, whether the
  initialization flag is needed.

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
- Which host will run the container, and whether it is Docker, systemd or Kubernetes (the guide assumes Docker).
