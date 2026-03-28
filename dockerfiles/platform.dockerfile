# Lightweight Dockerfile that packages a pre-built binary.
# The Go binary is compiled in CI (with build cache) and passed in as build context.
# This makes the Docker build near-instant (~5s) vs compiling inside Docker (~8 min).
#
# Two modes:
#   1. CI mode (fast): binary pre-built, just COPY + push
#   2. Full mode (fallback): compile inside Docker if no pre-built binary exists
ARG PREBUILT=false

# ---------- FULL BUILD STAGE (fallback when binary not pre-built) ----------
FROM golang:1.25.8 AS builder-full
ARG GOARCH=amd64
WORKDIR /build
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/crawbl ./cmd/crawbl

# ---------- PREBUILT STAGE (CI passes binary via build context) ----------
FROM scratch AS builder-prebuilt
COPY crawbl /out/crawbl

# ---------- SELECT STAGE ----------
FROM builder-${PREBUILT} AS builder

# ---------- RUNTIME STAGE ----------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/crawbl /crawbl
COPY migrations/ /migrations
USER nonroot:nonroot
ENTRYPOINT ["/crawbl"]
