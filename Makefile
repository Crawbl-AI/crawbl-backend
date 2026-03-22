.PHONY: fmt tidy test verify run-operator userswarm-operator-image-build userswarm-operator-image-push

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
IMAGE_REPOSITORY ?= registry.digitalocean.com/crawbl/crawbl-userswarm-operator
IMAGE_TAG ?= dev
PLATFORM ?= linux/amd64

export GOCACHE
export GOMODCACHE

fmt:
	gofmt -w ./api ./cmd ./internal

tidy:
	go mod tidy

test:
	go test ./...

verify: fmt test

run-operator:
	go run ./cmd/userswarm-operator

userswarm-operator-image-build:
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=0 ./scripts/userswarm-operator/build-and-push.sh

userswarm-operator-image-push:
	IMAGE_REPOSITORY=$(IMAGE_REPOSITORY) IMAGE_TAG=$(IMAGE_TAG) PLATFORM=$(PLATFORM) PUSH=1 ./scripts/userswarm-operator/build-and-push.sh
