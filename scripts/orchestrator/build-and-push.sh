#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-registry.digitalocean.com/crawbl/crawbl-orchestrator}"
IMAGE_TAG="${IMAGE_TAG:-dev}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-0}"

required_tools=(
  docker
)

for tool_name in "${required_tools[@]}"; do
  if ! command -v "$tool_name" >/dev/null 2>&1; then
    echo "missing required tool: $tool_name" >&2
    exit 1
  fi
done

IMAGE_REF="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
METADATA_DIR="$ROOT_DIR/.artifacts/orchestrator"
METADATA_FILE="$METADATA_DIR/${IMAGE_TAG}.metadata.json"
mkdir -p "$METADATA_DIR"

build_args=(
  buildx build
  --platform "$PLATFORM"
  --metadata-file "$METADATA_FILE"
  -f "$ROOT_DIR/dockerfiles/service.dockerfile"
  -t "$IMAGE_REF"
)

if [[ "$PUSH" == "1" ]]; then
  build_args+=(--push)
else
  build_args+=(--load)
fi

docker "${build_args[@]}" "$ROOT_DIR"

if [[ "$PUSH" == "1" ]]; then
  echo "==> pushed $IMAGE_REF"
else
  echo "==> built $IMAGE_REF locally"
fi
