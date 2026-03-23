.PHONY: help fmt tidy test verify run-operator docr-login userswarm-operator-image-build userswarm-operator-image-push

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
DOCR_REGISTRY ?= crawbl
IMAGE_REPOSITORY ?= registry.digitalocean.com/$(DOCR_REGISTRY)/crawbl-userswarm-operator
IMAGE_TAG ?= dev
PLATFORM ?= linux/amd64

export GOCACHE
export GOMODCACHE

help: ## Show available targets
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-32s\033[0m %s\n", $$1, $$2}'

fmt: ## Format Go source files
	gofmt -w ./api ./cmd ./internal

tidy: ## Sync go.mod and go.sum
	go mod tidy

test: ## Run the Go test suite
	go test ./...

verify: fmt test ## Run formatting and tests together

run-operator: ## Run the userswarm operator locally
	go run ./cmd/userswarm-operator

docr-login: ## Log Docker into the DigitalOcean Container Registry
	doctl registries login $(DOCR_REGISTRY)

userswarm-operator-image-build: ## Build the userswarm operator image locally
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=0 ./scripts/userswarm-operator/build-and-push.sh

userswarm-operator-image-push: docr-login ## Build and push the userswarm operator image to DOCR
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=1 ./scripts/userswarm-operator/build-and-push.sh
