# syntax=docker/dockerfile:1.7
#
# One image with two programs: OpenAI's tunnel-client-runtime (the entry point) and this
# server, which tunnel-client starts as a supervised stdio child.
#
# TUNNEL_RUNTIME_IMAGE has no default on purpose: pin an exact release and its digest, and
# verify the image's provenance first (see docs/deployment.md):
#
#   docker build \
#     --build-arg TUNNEL_RUNTIME_IMAGE=ghcr.io/openai/tunnel-client-runtime:<version>@sha256:<digest> \
#     --build-arg VERSION=<our version> -t gitlab-mcp-minimal:<tag> .
ARG GO_IMAGE=golang:1.26-alpine
ARG TUNNEL_RUNTIME_IMAGE

FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -mod=readonly -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/gitlab-mcp-minimal ./cmd/gitlab-mcp-minimal

FROM ${TUNNEL_RUNTIME_IMAGE}
COPY --from=build --chown=65532:65532 /out/gitlab-mcp-minimal /usr/bin/gitlab-mcp-minimal
USER 65532:65532
# tunnel-client starts this command and talks MCP to it over stdin/stdout. Mount the
# configuration at /etc/gitlab-mcp/config.json and the secrets under /run/secrets.
ENV MCP_COMMAND="/usr/bin/gitlab-mcp-minimal -config /etc/gitlab-mcp/config.json"
ENTRYPOINT ["/usr/bin/tunnel-client-runtime", "run"]
