# Thin wrapper around the repo-local `./crawbl` launcher.
# The launcher builds `bin/crawbl` on demand and keeps it fresh.

.PHONY: setup post-clone hooks build build-ci run run-db run-clean stop clean migrate test test-e2e fmt lint verify build-dev deploy-dev deploy-platform deploy-zeroclaw deploy-docs deploy-website ci-check

setup: hooks build
	./crawbl setup

post-clone:
	./scripts/post-clone.sh --force

hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-push ./crawbl

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

deploy-zeroclaw:
	./crawbl app deploy zeroclaw

deploy-docs:
	./crawbl app deploy docs

deploy-website:
	./crawbl app deploy website

ci-check: build test build-ci
