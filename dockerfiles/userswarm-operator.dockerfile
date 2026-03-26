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
    go build -trimpath -ldflags="-s -w" -o /out/userswarm-operator ./cmd/userswarm-operator

# ---------- RUNTIME STAGE ----------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=builder /out/userswarm-operator /userswarm-operator

USER nonroot:nonroot

ENTRYPOINT ["/userswarm-operator"]
