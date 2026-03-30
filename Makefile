# Thin wrapper around the repo-local `./crawbl` launcher.
# The launcher builds `bin/crawbl` on demand and keeps it fresh.

.PHONY: setup post-clone hooks build build-ci run run-db run-clean stop clean migrate test test-e2e fmt lint verify ci-check

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

ci-check: build test build-ci
