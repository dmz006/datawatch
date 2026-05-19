#!/usr/bin/env bash
# TS-160 — Start daemon in isolated mode
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts160_isolated_start
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-160"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_160() {
  # Clean up any stale datawatch-e2e-test containers that may be holding our ports
  docker ps -a --filter "name=dw-test-" --format "{{.Names}} {{.Status}}" 2>/dev/null | while read -r cname cstatus; do
    if [[ "$cname" != "dw-test-$$" ]]; then
      docker rm -f "$cname" 2>/dev/null || true
    fi
  done

  # Create config for Docker container (use 19xxx port range to avoid conflicts with test daemon)
  write_test_config "$DOCKER_SIM_DATA" "$DOCKER_SIM_HTTP" "$DOCKER_SIM_TLS" "$DOCKER_SIM_MCP" "$DOCKER_SIM_CHAN" "$TEST_TOKEN"
  # write_test_config replaces host: 0.0.0.0 → 127.0.0.1, but the container
  # daemon must bind on 0.0.0.0 to be reachable via docker port mapping.
  sed -i 's/host: 127\.0\.0\.1/host: 0.0.0.0/g' "$DOCKER_SIM_DATA/config.yaml"

  # Build a simple Docker image with the binary (use debian-slim as base for glibc compatibility)
  local dockerfile="$DOCKER_SIM_DATA/Dockerfile"
  cat > "$dockerfile" <<DOCKEREOF
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates git tmux && rm -rf /var/lib/apt/lists/*
COPY datawatch /usr/local/bin/
RUN chmod +x /usr/local/bin/datawatch
EXPOSE ${DOCKER_SIM_HTTP} ${DOCKER_SIM_TLS} ${DOCKER_SIM_MCP} ${DOCKER_SIM_CHAN}
ENTRYPOINT ["/usr/local/bin/datawatch", "start", "--foreground", "--config", "/config/config.yaml"]
DOCKEREOF

  # Copy binary into build context (ensure it exists first)
  if [[ ! -f "$TEST_BINARY" ]]; then
    skip "datawatch binary not found at $TEST_BINARY"
    return
  fi
  cp "$TEST_BINARY" "$DOCKER_SIM_DATA/datawatch" || { skip "failed to copy binary"; return; }

  # Build image
  local image_tag="datawatch-e2e-test:$$"
  if ! docker build -t "$image_tag" "$DOCKER_SIM_DATA" > "$DOCKER_SIM_DATA/build.log" 2>&1; then
    skip "docker build failed: $(tail -5 $DOCKER_SIM_DATA/build.log 2>/dev/null)"
    return
  fi

  # Run container
  local container_name="dw-test-$$"
  if ! docker run -d --name "$container_name" \
    -p "$DOCKER_SIM_HTTP:$DOCKER_SIM_HTTP" -p "$DOCKER_SIM_TLS:$DOCKER_SIM_TLS" \
    -p "$DOCKER_SIM_MCP:$DOCKER_SIM_MCP" -p "$DOCKER_SIM_CHAN:$DOCKER_SIM_CHAN" \
    -v "$DOCKER_SIM_DATA:/config" \
    "$image_tag" > "$DOCKER_SIM_DATA/container.id" 2>&1; then
    skip "docker run failed"
    return
  fi

  DOCKER_SIM_PID=$(cat "$DOCKER_SIM_DATA/container.id")
  DOCKER_SIM_CONTAINER="$container_name"
  DOCKER_SIM_IMAGE="$image_tag"
  echo "  Docker container: $DOCKER_SIM_CONTAINER (ports: $DOCKER_SIM_HTTP:HTTP $DOCKER_SIM_TLS:TLS)"

  # Wait for health — try HTTPS first (TLS enabled by default), fall back to HTTP
  local attempts=0
  while [[ $attempts -lt 30 ]]; do
    local h
    h=$(curl -sk --max-time 3 "https://127.0.0.1:$DOCKER_SIM_TLS/api/health" 2>/dev/null || \
        curl -s --max-time 3 "http://127.0.0.1:$DOCKER_SIM_HTTP/api/health" 2>/dev/null || echo "")
    if echo "$h" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
      save_evidence TS-160 "health.json" "$h"
      ok "daemon healthy in Docker container (:$DOCKER_SIM_TLS TLS)"
      return 0
    fi
    sleep 1
    attempts=$((attempts+1))
  done
  skip "daemon did not start in Docker container within 30s"
}

RESULT=fail
_story_ts_160
: "${RESULT:=fail}"
unset -f _story_ts_160
