#!/usr/bin/env bash
# Live daemon tests: provision fixture containers on a Docker daemon,
# run the tests against them, and clean up afterwards.

set -euo pipefail

FIXTURES="iw-live-latest iw-live-pinned iw-live-family-pg iw-live-family iw-live-composite"

if ! docker info >/dev/null 2>&1; then
	echo "skipping live daemon tests: no reachable Docker daemon"
	exit 0
fi

docker rm -f $FIXTURES >/dev/null 2>&1 || true
trap 'docker rm -f $FIXTURES >/dev/null 2>&1 || true' EXIT

docker run -d --name iw-live-latest --entrypoint tail --label image-watch.live-fixture=true alpine:latest -f /dev/null >/dev/null
docker run -d --name iw-live-pinned --entrypoint tail --label image-watch.live-fixture=true nginx:1.28.2 -f /dev/null >/dev/null
docker run -d --name iw-live-family-pg --entrypoint tail --label image-watch.live-fixture=true postgres:15 -f /dev/null >/dev/null
docker run -d --name iw-live-family --entrypoint tail --label image-watch.live-fixture=true nginx:1 -f /dev/null >/dev/null
docker run -d --name iw-live-composite --entrypoint tail --label image-watch.live-fixture=true postgres:15-alpine -f /dev/null >/dev/null

go test -count=1 -tags=live ./internal/runtime/docker
