# GitLab hardening checklist

The server's checks are defence in depth; GitLab is the real boundary. Do these before enabling the write tools.
Menu names are for GitLab 18.8 and may differ slightly (**VERIFY** against your instance).

## 1. Scope

- [ ] Allow-list only the projects the agent needs. Start with one disposable project (`docs/e2e-test.md`).
- [ ] Use a **project access token** per project, not a group token or a personal token of a human.

## 2. The token

- [ ] Role **Developer**. Never Maintainer or Owner.
- [ ] Scope **`read_api`** while write tools are off; **`api`** only when you enable them (`read_api` cannot write).
- [ ] A short expiry (for example 30 days) and a calendar reminder to rotate it. Store it only in the secret file.
- [ ] Know where to revoke it: the project's access token settings.

## 3. The default branch

In the repository settings, protected branches:

- [ ] The default branch is protected. **Allowed to push and merge: No one.** **Allowed to merge: Maintainers** only, so
  the Developer-role bot can neither push to it nor merge into it.
- [ ] Do **not** protect `agent/*` (or the prefix you chose). The server refuses protected branches, so protecting the
  agent prefix would make every commit fail.

## 4. Review

In merge request settings:

- [ ] Require at least one approval from a person other than the author, and prevent approval by the author and by
  committers.
- [ ] Remove approvals when new commits are pushed.
- [ ] Require approval from code owners for the default branch.

## 5. CODEOWNERS

The server already refuses to write `.gitlab-ci.yml`, `.gitlab/` and `CODEOWNERS`. Cover more in `CODEOWNERS`, because
CI can include files from other places:

- [ ] `.gitlab-ci.yml`, `.gitlab/`, `CODEOWNERS`, and any file the pipeline includes.
- [ ] Scripts, `Makefile`, `Dockerfile`s and deployment manifests that CI executes.
- [ ] A human owner for each, so changes to them need that person's approval.

## 6. CI/CD: agent-written code runs before anyone reviews it

Pipelines run on the agent's branch as soon as it is pushed. Even without changing `.gitlab-ci.yml`, the agent can
change tests, scripts or build files that the pipeline runs. Treat that as untrusted code execution:

- [ ] Mark every secret CI/CD variable as **Protected** (only exposed to protected branches and tags) and **Masked**.
- [ ] Run deploy and release jobs only on protected refs (for example `rules: if: $CI_COMMIT_REF_PROTECTED == "true"`).
- [ ] Use runners for unprotected branches that hold no credentials and cannot reach production networks.
- [ ] Restrict the project's protected runners to protected refs.
- [ ] Do not rely on file deny-lists alone; the checks above are what stop secret exfiltration through CI.

## 7. Visibility and alerts

- [ ] Watch the project's audit events and the bot user's activity.
- [ ] Alert on any push to a protected branch by the bot (there should never be one).
- [ ] Read the audit log of this server (`docs/deployment.md`, logs).
- [ ] Review every merge request the bot opens, including its CI results, before merging.

## 8. Data you are sending out

Everything a tool returns (files, diffs, job logs) goes to the model provider. Keep secrets out of repositories and
masked in CI logs, and restrict the allow-list accordingly.
