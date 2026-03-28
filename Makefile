.PHONY: help deps-lint fmt tidy test verify setup stop clean run run-clean migrate test-e2e run-server run-operator lint lint-fix

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
ENV_FILE ?= .env

export GOCACHE
export GOMODCACHE
export ENV_FILE

.env:
	cp .env.example .env

help: ## Show available targets
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-32s\033[0m %s\n", $$1, $$2}'

deps-lint: ## Install golangci-lint locally if it is missing
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint already installed: $$(golangci-lint version 2>/dev/null || echo unknown)"; \
	else \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi

fmt: ## Format Go source files
	gofmt -w ./api ./cmd ./internal

lint: deps-lint ## Run golangci-lint
	golangci-lint run ./...

lint-fix: deps-lint ## Run golangci-lint with auto-fix
	golangci-lint run ./... --fix

tidy: ## Sync go.mod and go.sum
	go mod tidy

test: ## Run the Go test suite
	go test ./...

verify: fmt lint test ## Run formatting, linting, and tests together

setup: .env ## Start Postgres and run local migrations
	@docker compose --profile database up -d
	until docker compose exec -T postgresdb pg_isready -h postgresdb; do sleep 1; done
	@docker compose --profile database --profile migration build migrations
	@docker compose --profile database --profile migration run --rm migrations

stop: .env ## Stop local Docker Compose services
	@docker compose --profile default --profile database down --remove-orphans

clean: ## Remove the local Postgres volume
	@docker volume rm -f crawbl-backend_db-data

run: stop setup ## Start the local Postgres-backed orchestrator stack
	@docker compose --profile default --profile database up -d --build --remove-orphans

run-clean: stop clean setup ## Recreate the local Postgres-backed orchestrator stack from scratch
	@docker compose --profile default --profile database up -d --build --remove-orphans

migrate: .env ## Run the local orchestrator migrations once
	@docker compose --profile database --profile migration build migrations
	@docker compose --profile database --profile migration run --rm migrations

test-e2e: ## Run e2e tests against the dev cluster
	go run ./cmd/crawbl test e2e --base-url https://dev.api.crawbl.com

run-server: ## Run the orchestrator HTTP server locally
	go run ./cmd/crawbl platform orchestrator

run-operator: ## Run the userswarm operator locally
	go run ./cmd/crawbl platform operator
