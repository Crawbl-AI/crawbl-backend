# ---------- BUILD STAGE ----------
FROM golang:1.25.8 AS builder

ARG GOARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go env -w GOPROXY=direct && go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux CGO_ENABLED=0 GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/orchestrator ./cmd/orchestrator

# ---------- RUNTIME STAGE ----------
FROM debian:bullseye-slim AS runtime

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && update-ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /

COPY --from=builder /out/orchestrator /usr/local/bin/orchestrator
COPY --from=builder /build/migrations /migrations

ENTRYPOINT ["/usr/local/bin/orchestrator", "migrate"]
