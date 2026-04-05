# syntax=docker/dockerfile:1.7
#
# Dockerfile for the crawbl-agent-runtime binary.
#
# Builds the per-workspace agent runtime that replaces agent runtime in Phase 2.
# The image is intentionally minimal — distroless nonroot base, only the
# runtime binary copied in, no config files (config comes from mounted
# ConfigMap at /config/runtime.yaml and env vars injected by the K8s Pod
# spec).
#
# Acceptance criteria from plan §7:
#   - Final image ≤ 60 MB (AC #3)
#   - Runs as nonroot (uid 65532)
#   - Exposes gRPC on port 42618
#
# Build from crawbl-backend/ root:
#   docker build -f dockerfiles/agent-runtime.dockerfile .

FROM golang:1.25.8 AS builder

WORKDIR /build

# Cache-friendly dependency copy first.
COPY go.mod go.sum ./
COPY vendor/ vendor/

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY proto/ proto/
COPY migrations/ migrations/

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -mod=vendor -trimpath -ldflags="-s -w -X main.version=$(date -u +%Y%m%dT%H%M%SZ)" \
    -o /out/crawbl-agent-runtime ./cmd/crawbl-agent-runtime

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/crawbl-agent-runtime /crawbl-agent-runtime

# gRPC listener port (see plan §6.4).
EXPOSE 42618

USER 65532:65532

ENTRYPOINT ["/crawbl-agent-runtime"]
