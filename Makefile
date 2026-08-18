# image-watch build and test entrypoints.

SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: test test-live image

build:
	go build -o bin/image-watch ./cmd/image-watch

test:
	go test ./...

# Live integration tests: Docker Hub, then a Docker daemon.
test-live:
	go test -count=1 -tags=live ./internal/registry/distribution
	./scripts/test-live.sh

image:
	docker build --build-arg VERSION="$(VERSION)" -t image-watch:"$(VERSION)" .