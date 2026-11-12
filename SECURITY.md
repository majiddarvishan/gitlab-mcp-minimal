# Security policy

This project handles a credential that can write to a source-code host, so security reports are taken seriously.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting (the repository's **Security** tab,
then **Report a vulnerability**). If that option is not available, contact the maintainer privately through GitHub.

Helpful details: the affected commit or version, the impact, and steps to reproduce.

## Supported versions

The project is pre-release. Only the `main` branch is supported.

## Scope

In scope: this server's own code, its configuration handling, and its container packaging.

Out of scope: GitLab-side configuration (token scope, branch protection, CI settings), and vulnerabilities in GitLab,
ChatGPT or OpenAI's `tunnel-client` themselves, which should be reported upstream.

The design goals and threat model are summarised in [`.claude/DECISIONS.md`](.claude/DECISIONS.md).
