# ---------- BUILD STAGE ----------
FROM golang:1.25.8 AS builder

ARG GOARCH=amd64
ARG CGO=0
ARG LDFLAGS=""

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt update && apt install -y bash-static

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go env -w GOPROXY=direct && go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux CGO_ENABLED=${CGO} GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w ${LDFLAGS}" -o /out/orchestrator ./cmd/orchestrator

# ---------- RUNTIME STAGE ----------
FROM debian:bullseye-slim AS runtime

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && update-ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/orchestrator orchestrator
COPY --from=builder /bin/bash-static /bin/bash
COPY --from=builder /build/migrations /app/migrations

ENTRYPOINT ["/app/orchestrator", "server"]
