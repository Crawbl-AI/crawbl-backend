# Full build Dockerfile — compiles Go binary inside Docker.
# Used for local builds or when CI pre-build is not available.
#
# Usage:
#   crawbl app build platform --tag dev
#   docker build -f dockerfiles/platform-full.dockerfile .
FROM golang:1.25.8 AS builder

ARG GOARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY vendor-patches/ vendor-patches/
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/

# Apply vendor patches (fixes third-party bugs without forking upstream).
RUN apt-get update -qq && apt-get install -y -qq patch >/dev/null 2>&1; \
    for p in vendor-patches/*.patch; do [ -f "$p" ] && patch -p1 < "$p"; done

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/crawbl ./cmd/crawbl

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/memory-process ./cmd/crawbl/jobs/memory-process

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/memory-maintain ./cmd/crawbl/jobs/memory-maintain

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=builder /out/crawbl /crawbl
COPY --from=builder /out/memory-process /memory-process
COPY --from=builder /out/memory-maintain /memory-maintain
COPY --from=builder /build/migrations /migrations

USER nonroot:nonroot

ENTRYPOINT ["/crawbl"]
