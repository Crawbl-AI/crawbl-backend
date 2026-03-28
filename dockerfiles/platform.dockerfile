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
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/crawbl ./cmd/crawbl

# ---------- RUNTIME STAGE ----------
# Distroless static image: ca-certificates + tzdata, no shell, no glibc.
# Works because CGO_ENABLED=0 produces a fully static binary.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=builder /out/crawbl /crawbl
COPY --from=builder /build/migrations /migrations

USER nonroot:nonroot

ENTRYPOINT ["/crawbl"]
