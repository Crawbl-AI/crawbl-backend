# CI mode: packages a pre-built binary into distroless.
# The Go binary is compiled in CI (with build cache) and placed in the build context.
# Docker just COPYs it — takes ~5 seconds.
#
# Usage:
#   CI:    docker build -f dockerfiles/platform.dockerfile .   (crawbl binary in context root)
#   Local: go build ... -o crawbl && docker build ...
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY crawbl /crawbl
COPY migrations/ /migrations
USER nonroot:nonroot
ENTRYPOINT ["/crawbl"]
