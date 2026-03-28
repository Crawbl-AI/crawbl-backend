# ---------- BUILD STAGE ----------
FROM golang:1.25.8 AS builder

ARG GOARCH=amd64

WORKDIR /build

# Copy vendored dependencies first (cached layer — changes rarely).
COPY go.mod go.sum ./
COPY vendor/ vendor/

# Copy source code (changes frequently — invalidates only this layer).
COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/crawbl ./cmd/crawbl

# ---------- RUNTIME STAGE ----------
# Distroless static image: ca-certificates + tzdata, no shell, no glibc.
# Works because CGO_ENABLED=0 produces a fully static binary.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=builder /out/crawbl /crawbl
COPY --from=builder /build/migrations /migrations

USER nonroot:nonroot

ENTRYPOINT ["/crawbl"]
