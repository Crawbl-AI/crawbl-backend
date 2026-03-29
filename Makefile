# Thin wrapper around `crawbl` CLI.
# All commands live in cmd/crawbl/ — this file exists for muscle memory.

.PHONY: setup run run-clean stop clean migrate test test-e2e fmt lint verify

setup:
	go run ./cmd/crawbl setup

run:
	go run ./cmd/crawbl dev start

run-clean:
	go run ./cmd/crawbl dev start --clean

stop:
	go run ./cmd/crawbl dev stop

clean:
	go run ./cmd/crawbl dev reset

migrate:
	go run ./cmd/crawbl dev migrate

test:
	go run ./cmd/crawbl test unit

test-e2e:
	go run ./cmd/crawbl test e2e --base-url https://dev.api.crawbl.com

fmt:
	go run ./cmd/crawbl dev fmt

lint:
	go run ./cmd/crawbl dev lint

verify:
	go run ./cmd/crawbl dev verify
