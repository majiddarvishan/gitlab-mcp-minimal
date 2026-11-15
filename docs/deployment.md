# Deployment

How to run the server so that ChatGPT can reach a GitLab on a private network. It is written from the public
documentation and source of OpenAI's `tunnel-client` (the Secure MCP Tunnel client). Statements marked **VERIFY** have
not been tried on a real installation yet; that is what T4.5 (`docs/e2e-test.md`) is for.

## How the pieces fit

```
ChatGPT -> OpenAI tunnel service <- outbound HTTPS (api.openai.com:443) <- tunnel-client-runtime
                                                                          |  stdin/stdout (MCP)
                                                          gitlab-mcp-minimal (child process)
                                                                          |  HTTPS, GitLab REST API
                                                                       your GitLab
```

- `tunnel-client-runtime` is the narrow runtime flavour of OpenAI's tunnel client. Its command surface is `run`, help and
  version. It connects outbound to OpenAI and starts this server as a supervised child process.
- This server speaks MCP over the child's stdin/stdout and calls the GitLab REST API.
- Nothing listens on a public interface. The runtime exposes a health endpoint that defaults to `127.0.0.1:8080`.

## Network requirements

- Outbound HTTPS from the container to `api.openai.com:443` (paths `/v1/tunnels/*`).
- Outbound from the container to your GitLab host and port. The GitLab client never uses a proxy (D15), so GitLab must
  be reachable directly.
- No inbound ports. Publish the health port only to a trusted network.
- Restrict the container's egress to exactly those two destinations.

## 1. OpenAI side (once)

The pages are listed in the tunnel-client README (`platform.openai.com/settings/organization/...`: `tunnels`,
`people/roles`, `api-keys`).

1. Create a **tunnel** and note its id (`tunnel_` followed by 32 characters). Creating one needs the Tunnels *Manage*
   permission; the default Owner role does not include it, so create a role with Tunnels Manage and Use and assign it
   to yourself.
2. Create a **runtime API key** for the daemon with Tunnels *Read* and *Use*. Do not use an admin key for the daemon.
3. Save the key as `secrets/openai_runtime_key`.

## 2. GitLab side (once)

Follow `docs/gitlab-hardening.md`, then save the project access token as `secrets/gitlab_token`.

## 3. Choose and verify the tunnel-client-runtime image

OpenAI publishes `ghcr.io/openai/tunnel-client-runtime` for linux/amd64 and linux/arm64, with an SBOM attestation and
signed provenance. The image is Alpine-based and runs as uid 65532.

1. Pick an exact release. Pre-releases publish exact version tags only; `latest` and minor aliases never move for them.
2. Verify its provenance with the GitHub CLI (**VERIFY** for this image name; the upstream docs show the command for
   the full image):
   ```sh
   gh attestation verify oci://ghcr.io/openai/tunnel-client-runtime:<version> -R openai/tunnel-client
   ```
3. Record its digest (`docker buildx imagetools inspect ghcr.io/openai/tunnel-client-runtime:<version>`) and use
   `<version>@sha256:<digest>` everywhere.
4. Scan the exact image you deploy. Upstream's own vulnerability report marks OCI images as not scanned.

## 4. Build our image

```sh
docker build \
  --build-arg TUNNEL_RUNTIME_IMAGE=ghcr.io/openai/tunnel-client-runtime:<version>@sha256:<digest> \
  --build-arg VERSION=<our version> \
  -t gitlab-mcp-minimal:<tag> .
```

The `Dockerfile` has no default for `TUNNEL_RUNTIME_IMAGE`, so a build without a pin fails instead of silently using a
moving tag. It builds this server from the repository (`go mod verify` runs first) and adds it to the runtime image.

## 5. Configure

```sh
cp config.example.json config.json
```

Edit `base_url` (https), the project allow-list and, for a private CA, `ca_file` (mount the PEM and use its absolute
path). Keep the token path `/run/secrets/gitlab_token`. **Leave `enable_write_tools` off for the first run.**
`config.json` and `secrets/` are in `.gitignore` and `.dockerignore`; never commit them.

The secret files must be readable by uid 65532: `chown 65532:65532 secrets/*` and `chmod 0400 secrets/*`.

## 6. Run

```sh
cp deploy/docker-compose.example.yml docker-compose.yml   # set CONTROL_PLANE_TUNNEL_ID and the image tag
docker compose up -d
docker compose logs -f gitlab-mcp
```

The compose example runs read-only, drops all capabilities, forbids new privileges, and passes the OpenAI key as a
file (`--control-plane.api-key=file:...`), so neither secret is in the environment that the MCP child inherits.
The MCP command comes from `MCP_COMMAND` in the image.

Check health from inside the container: `wget -qO- http://127.0.0.1:8080/healthz` and `/readyz`. `readyz` may answer
503 until the first poll completes (**VERIFY**). Create or verify the ChatGPT connector only while the daemon is
healthy (`docs/chatgpt-connector.md`).

## Operating rules

- **Exactly one instance per tunnel id.** This is an upstream limit for stdio servers: two instances sharing a tunnel
  id start two separate children, and an initialization can reach one while the next call reaches the other. It
  includes the overlap during a restart. Stop the old container before starting its replacement; on Kubernetes use
  `replicas: 1` and `strategy.type: Recreate`. For independent instances use distinct tunnel ids.
- **Restarts.** By default `tunnel-client` restarts the child after an unexpected exit. Calls in flight can fail.
- **Logs.** `tunnel-client` logs as JSON. Our audit lines go to the child's stderr; whether they appear in the container
  log depends on how the runtime forwards child stderr (**VERIFY**). Confirm it before relying on the audit log, and if
  they do not appear, route them (for example with a wrapper that writes to a file on a mounted volume).
- **Upgrades.** Rebuild with the new pinned digest (Dependabot proposes base image updates), then stop old, start new.
- **Turning on write tools.** Set `enable_write_tools` to `true` in `config.json`, make sure the token has the `api`
  scope, and restart.
- **systemd or Kubernetes.** Upstream documents both (`docs/deployment/systemd-vm.md`, `kubernetes-dedicated.md`,
  `kubernetes-sidecar.md`). The same rules apply: stdio via `MCP_COMMAND`, secrets as files, one instance per tunnel.

## If calls fail with `mcp_initialization_required`

For stdio servers `tunnel-client` rejects legacy requests such as `tools/call` until the caller has completed
`initialize` and sent `notifications/initialized` (HTTP 409, JSON-RPC code -32002). If ChatGPT does not send the
notification, add `--mcp.stdio-send-initialized-notification` to the `command:` list so the runtime sends it after the
forwarded `initialize` response. Requests using protocol version 2026-07-28 or later with client capabilities in
`params._meta` skip this guard. This server keeps no per-session state, so either mode is safe for it. **VERIFY** in T4.5.

## Alternatives considered (D18)

- **Embed the tunnel client in this process** through its Go SDK and the MCP SDK's in-memory transport. It would give a
  single binary, but it puts OpenAI's network client and the GitLab token in one process and ties the release cadence of
  both together.
- **HTTP instead of stdio** (`--mcp.server-url`, even over a Unix socket). It would add a listener, which D2 rules out.
- **Release ZIP and systemd** instead of the image. Valid; upstream publishes ZIPs with SHA-256 digests and evidence
  files. The image path was chosen because it gives one pinned, attested artifact.
