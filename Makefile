# Thin wrapper around the repo-local `./crawbl` launcher.
# The launcher builds `bin/crawbl` on demand and keeps it fresh.

.PHONY: setup post-clone hooks build build-ci run run-db run-clean stop clean migrate test test-e2e fmt lint verify build-dev deploy-dev deploy-platform deploy-agent-runtime deploy-docs deploy-website ci-check generate generate-tools-install

setup: hooks build
	./crawbl setup

post-clone:
	./scripts/post-clone.sh --force

hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-push .githooks/pre-commit ./crawbl

build:
	./crawbl --version >/dev/null

build-ci:
	mkdir -p .artifacts/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -mod=vendor -trimpath -ldflags="-s -w" -buildvcs=false \
		-o .artifacts/bin/crawbl-linux-amd64 ./cmd/crawbl

run:
	./crawbl dev start

run-db:
	./crawbl dev start --database-only

run-clean:
	./crawbl dev start --clean

stop:
	./crawbl dev stop

clean:
	./crawbl dev reset

migrate:
	./crawbl dev migrate

test:
	./crawbl test unit

test-e2e:
	./crawbl test e2e --base-url https://dev.api.crawbl.com

fmt:
	./crawbl dev fmt

lint:
	./crawbl dev lint

verify:
	./crawbl dev verify

# Build the platform image with auto-calculated semver tag.
build-dev:
	./crawbl app build platform

# Build + push + update ArgoCD for all backend components (semver auto-calculated).
deploy-dev:
	./crawbl app deploy all

# Per-component deploy targets (semver auto-calculated).
deploy-platform:
	./crawbl app deploy platform

deploy-agent-runtime:
	./crawbl app deploy agent-runtime

deploy-docs:
	./crawbl app deploy docs

deploy-website:
	./crawbl app deploy website

ci-check: generate build test build-ci

# ---------------------------------------------------------------------------
# Protobuf / gRPC codegen for the crawbl-agent-runtime.
#
# Generated .pb.go / *_grpc.pb.go files are GITIGNORED — they are derived
# artifacts and regenerated from proto/agentruntime/v1/*.proto on every
# fresh clone via `make generate`. `make ci-check` depends on generate so
# CI always has them before building.
#
# Requires on PATH:
#   - protoc (pinned in .mise.toml; `mise install` to provision)
#   - protoc-gen-go
#   - protoc-gen-go-grpc
# Run `make generate-tools-install` to install the Go plugins if missing.
# ---------------------------------------------------------------------------
generate:
	@command -v protoc >/dev/null 2>&1 || { echo "ERROR: protoc not found. Run 'mise install' in crawbl-backend/ or 'brew install protobuf'."; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || { echo "ERROR: protoc-gen-go not found in PATH ($$PATH). Run 'make generate-tools-install'."; exit 1; }
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || { echo "ERROR: protoc-gen-go-grpc not found in PATH. Run 'make generate-tools-install'."; exit 1; }
	@mkdir -p internal/agentruntime/proto/v1
	protoc \
		--go_out=. --go_opt=module=github.com/Crawbl-AI/crawbl-backend \
		--go-grpc_out=. --go-grpc_opt=module=github.com/Crawbl-AI/crawbl-backend \
		--proto_path=proto \
		proto/agentruntime/v1/runtime.proto \
		proto/agentruntime/v1/memory.proto
	@echo "generated: internal/agentruntime/proto/v1/*.pb.go"

generate-tools-install:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "installed: protoc-gen-go, protoc-gen-go-grpc into $$(go env GOPATH)/bin"
