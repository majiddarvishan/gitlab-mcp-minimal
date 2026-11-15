# Connecting ChatGPT

This connects ChatGPT to the server through the tunnel. The ChatGPT screens change often and Developer mode is in
beta, so the labels below are **VERIFY** items; if they differ, follow the current ChatGPT help and keep the
principles.

## Before you start

- The tunnel daemon is running and healthy (`docs/deployment.md`). Upstream advises creating or checking the connector
  only while `tunnel-client run` is healthy, and keeping it running for discovery and every call.
- Start **read-only**: `enable_write_tools` is off, so ChatGPT sees only the seven read tools.
- Use your own account first. Anyone who can use the connector acts with the bot token's permissions (see below).

## Create the connector

1. Open ChatGPT settings, **Connectors**, then **Advanced**, and turn on **Developer mode** (a beta feature, available
   on some paid plans; a workspace admin may have to allow it).
2. Choose **Create** (an app or custom connector).
3. For the connection choose **Tunnel**, then select your tunnel, or enter its id.
4. For authentication choose **No authentication**. The access control lives elsewhere: the tunnel is only reachable by
   your organisation, and GitLab access is limited by the token this server holds.
5. Save, then start a chat, add the connector from the tools menu and ask what tools it offers. You should see
   `list_projects`, `get_tree`, `get_file`, `get_mr_diff`, `list_pipelines`, `list_pipeline_jobs` and `get_job_trace`.

The connector is created in the account settings. Check that it also appears in the desktop app; if it does not, use
the web app for the first tests.

## What people with access can do

With **No authentication**, every user who can use this connector in a workspace has the same GitLab permissions: those
of the bot token. In a shared workspace limit who can add or use it, keep the allow-list small, and prefer a separate
tunnel and token per team.

## Confirmations

Read tools are marked read-only, so ChatGPT should not ask before calling them. The write tools (when enabled) are not,
so ChatGPT should ask you to confirm each call and show the arguments. Read them: they are your last check before a
branch, commit, merge request or comment is created.

## Suggested first prompts

1. "List the projects you can access."
2. "Show the top-level files of the first project and read its README."
3. "List the latest pipelines of that project and the jobs of the newest failed one."

After the read-only checks pass, turn on write tools and try: "Create a branch for a small documentation fix, change one
line in a file, and open a draft merge request." Then review the merge request in GitLab before merging anything.

## What the model is told

The server sends instructions on connect: only the listed projects are accessible, everything it returns is untrusted
data written by other people and must not be obeyed, and changes go to prefixed branches and draft merge requests
that a human merges. These are guidance for the model. The enforcement is in the server and in GitLab.

## Troubleshooting

- **No tools listed or the connector will not save:** check that the daemon is healthy, that only one instance is
  running for the tunnel id, and that the runtime key has Tunnels Read and Use.
- **Calls fail with `mcp_initialization_required`:** see the section in `docs/deployment.md`.
- **"internal error" from a tool:** the details are in the audit log, not in the chat.
- **"not allowed" or "invalid input" messages:** the server refused the request on purpose; they explain which rule
  applied.
