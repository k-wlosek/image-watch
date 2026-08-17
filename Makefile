# image-watch build and test entrypoints.

SHELL := /bin/bash

.PHONY: test test-live

build:
	go build -o bin/image-watch ./cmd/image-watch

test:
	go test ./...

# Live integration tests: Docker Hub, then a Docker daemon.
test-live:
	go test -count=1 -tags=live ./internal/registry/distribution
	./scripts/test-live.sh