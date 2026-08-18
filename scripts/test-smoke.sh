#!/usr/bin/env bash
# End-to-end image-watch deployment smoke test.
set -euo pipefail

IMG=${IMG:-image-watch:test}
NAME=image-watch-test
VSTATE=image-watch-test-data
DEMOS=(iw-demo-nginx iw-demo-pg iw-demo-pg-cmp iw-demo-alpine)

docker build -t "$IMG" .

# Ensure a clean state before the test.
docker rm -f "$NAME" "${DEMOS[@]}" >/dev/null 2>&1 || true
docker volume rm "$VSTATE" >/dev/null 2>&1 || true

docker run -d --name iw-demo-nginx nginx:1.28.2
docker run -d --name iw-demo-pg -e POSTGRES_PASSWORD=test postgres:15
docker run -d --name iw-demo-pg-cmp -e POSTGRES_PASSWORD=test postgres:15-alpine
docker run -d --name iw-demo-alpine alpine:latest tail -f /dev/null

read -r -d '' TESTCONF <<'EOF' || true
check_interval: 30s
policy:
  family_advancement: true
  other_platform: true
notifications:
  mode: batch
  targets:
    - type: stdout
metrics:
  listen: "0.0.0.0:9090"
EOF
printf '%s\n' "$TESTCONF" >/tmp/iw-test-config.yaml

docker run -d --name "$NAME" \
	-v /var/run/docker.sock:/var/run/docker.sock:ro \
	-v "$VSTATE":/var/lib/image-watch \
	-v /tmp/iw-test-config.yaml:/etc/image-watch/config.yaml:ro \
	-p 127.0.0.1:9090:9090 \
	"$IMG"

# Wait for the first cycle to complete
sleep 8
echo "container status:"
docker ps --filter name="$NAME" --format '{{.Names}}  {{.Status}}'
echo "healthcheck:"
curl -sf http://127.0.0.1:9090/healthz && echo
echo "image-watch cycle output:"

sleep 4
# Since we're building image-watch from source, expecting 1 failed check
# (image-watch:test isn't listed on Docker Hub, so the check will fail to fetch its manifest).
docker logs "$NAME" 2>&1 | tail -40

echo "metrics"
curl -sf http://127.0.0.1:9090/metrics |
	grep -E 'image_watch'

# Cleanup
echo "cleaning up test containers and volume"
docker rm -f "$NAME" "${DEMOS[@]}" || true
docker volume rm "$VSTATE" || true
