.PHONY: help deps-venom deps-lint fmt tidy test verify setup stop clean run run-clean migrate test-e2e test-e2e-one run-server run-operator docr-login userswarm-operator-image-build userswarm-operator-image-push orchestrator-image-build orchestrator-image-push lint lint-fix

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
DOCR_REGISTRY ?= crawbl
ORCHESTRATOR_IMAGE_REPOSITORY ?= registry.digitalocean.com/$(DOCR_REGISTRY)/crawbl-orchestrator
IMAGE_REPOSITORY ?= registry.digitalocean.com/$(DOCR_REGISTRY)/crawbl-userswarm-operator
IMAGE_TAG ?= dev
ORCHESTRATOR_IMAGE_TAG ?= $(IMAGE_TAG)
ORCHESTRATOR_BUILD_SCRIPT ?= ./scripts/orchestrator/build-and-push.sh
PLATFORM ?= linux/amd64
ENV_FILE ?= .env
E2E_DIR ?= $(CURDIR)/e2e
VENOM_VERSION ?= v1.3.0

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

deps-venom: ## Install ovh/venom locally if it is missing
	@if command -v venom >/dev/null 2>&1; then \
		echo "venom already installed: $$(venom version 2>/dev/null || echo unknown)"; \
	else \
		echo "Installing venom $(VENOM_VERSION)..."; \
		OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
		ARCH=$$(uname -m); \
		case $$ARCH in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; esac; \
		DEST=$${VENOM_INSTALL_DIR:-$(HOME)/bin}; \
		mkdir -p $$DEST; \
		curl -sSfL "https://github.com/ovh/venom/releases/download/$(VENOM_VERSION)/venom.$$OS-$$ARCH" -o "$$DEST/venom"; \
		chmod +x "$$DEST/venom"; \
		echo "venom $(VENOM_VERSION) installed to $$DEST/venom"; \
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

test-e2e: deps-venom run-clean ## Run the minimal orchestrator workflow against the local Docker stack
	@attempts=0; until curl -fsS http://127.0.0.1:7171/v1/health >/dev/null 2>/dev/null; do \
		attempts=$$((attempts + 1)); \
		if [ $$attempts -ge 60 ]; then \
			echo "orchestrator health check did not become ready in time"; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@PATH="$(HOME)/bin:$(PATH)" venom run $(E2E_DIR)/tests/ --output-dir=$(E2E_DIR) \
		&& EXIT=0 || EXIT=$$?; \
	$(MAKE) stop; \
	exit $$EXIT

test-e2e-one: deps-venom run-clean ## Run one Venom file from e2e/tests, e.g. FILE=01_orchestrator_smoke.yml
	@attempts=0; until curl -fsS http://127.0.0.1:7171/v1/health >/dev/null 2>/dev/null; do \
		attempts=$$((attempts + 1)); \
		if [ $$attempts -ge 60 ]; then \
			echo "orchestrator health check did not become ready in time"; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@PATH="$(HOME)/bin:$(PATH)" venom run $(E2E_DIR)/tests/$(FILE) --output-dir=$(E2E_DIR) \
		&& EXIT=0 || EXIT=$$?; \
	$(MAKE) stop; \
	exit $$EXIT

run-server: ## Run the orchestrator HTTP server locally
	go run ./cmd/orchestrator server

run-operator: ## Run the userswarm operator locally
	go run ./cmd/userswarm-operator operator

docr-login: ## Log Docker into the DigitalOcean Container Registry
	doctl registries login $(DOCR_REGISTRY)

userswarm-operator-image-build: ## Build the userswarm operator image locally
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=0 bash ./scripts/userswarm-operator/build-and-push.sh

userswarm-operator-image-push: docr-login ## Build and push the userswarm operator image to DOCR
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=1 bash ./scripts/userswarm-operator/build-and-push.sh

orchestrator-image-build: ## Build the orchestrator image locally
	IMAGE_REPOSITORY=$(ORCHESTRATOR_IMAGE_REPOSITORY) IMAGE_TAG=$(ORCHESTRATOR_IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=0 bash $(ORCHESTRATOR_BUILD_SCRIPT)

orchestrator-image-push: docr-login ## Build and push the orchestrator image to DOCR
	IMAGE_REPOSITORY=$(ORCHESTRATOR_IMAGE_REPOSITORY) IMAGE_TAG=$(ORCHESTRATOR_IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=1 bash $(ORCHESTRATOR_BUILD_SCRIPT)
