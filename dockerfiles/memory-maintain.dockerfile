FROM golang:1.25.8 AS builder

WORKDIR /build

COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY vendor-patches/ vendor-patches/
COPY cmd/ cmd/
COPY internal/ internal/

RUN apt-get update -qq && apt-get install -y -qq patch >/dev/null 2>&1; \
    for p in vendor-patches/*.patch; do [ -f "$p" ] && patch -p1 < "$p"; done

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/memory-maintain ./cmd/crawbl/jobs/memory-maintain

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/memory-maintain /memory-maintain

ENTRYPOINT ["/memory-maintain"]
