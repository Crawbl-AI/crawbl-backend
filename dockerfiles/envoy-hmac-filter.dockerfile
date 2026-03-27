# Build stage: compile Go source to WASM using TinyGo.
# TinyGo produces compact WASM binaries suitable for Envoy's proxy-wasm runtime.
FROM tinygo/tinygo:0.33.0 AS build

WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build the WASM binary.
COPY *.go ./
RUN tinygo build -o filter.wasm -scheduler=none -target=wasi .

# OCI image: single layer with just the .wasm file.
# Envoy Gateway pulls this image and extracts the WASM binary.
# Must be exactly one COPY instruction for Envoy Gateway OCI WASM support.
FROM scratch
COPY --from=build /src/filter.wasm /plugin.wasm
