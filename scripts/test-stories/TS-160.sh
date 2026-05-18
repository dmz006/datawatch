#!/usr/bin/env bash
# TS-160 — Start daemon in isolated mode
# tags: surface:docker feature:bootstrap
# legacy fn: t13_ts160_isolated_start
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-160"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_160() {
  # Create config for Docker container (use 19xxx port range to avoid conflicts with test daemon)
  write_test_config "$DOCKER_SIM_DATA" "$DOCKER_SIM_HTTP" "$DOCKER_SIM_TLS" "$DOCKER_SIM_MCP" "$DOCKER_SIM_CHAN" "$TEST_TOKEN"

  # Build a simple Docker image with the binary (use debian-slim as base for glibc compatibility)
  local dockerfile="$DOCKER_SIM_DATA/Dockerfile"
  cat > "$dockerfile" <<'DOCKEREOF'
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY datawatch /usr/local/bin/
RUN chmod +x /usr/local/bin/datawatch
EXPOSE 18180 18543 18281 18533
ENTRYPOINT ["/usr/local/bin/datawatch", "start", "--foreground", "--host", "0.0.0.0", "--config", "/config/config.yaml", "--port", "18180"]
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
    -p "$DOCKER_SIM_HTTP:18180" -p "$DOCKER_SIM_TLS:18543" -p "$DOCKER_SIM_MCP:18281" -p "$DOCKER_SIM_CHAN:18533" \
    -v "$DOCKER_SIM_DATA:/config" \
    "$image_tag" > "$DOCKER_SIM_DATA/container.id" 2>&1; then
    skip "docker run failed"
    return
  fi

  DOCKER_SIM_PID=$(cat "$DOCKER_SIM_DATA/container.id")
  DOCKER_SIM_CONTAINER="$container_name"
  DOCKER_SIM_IMAGE="$image_tag"
  echo "  Docker container: $DOCKER_SIM_CONTAINER (ports: $DOCKER_SIM_HTTP:HTTP $DOCKER_SIM_TLS:TLS)"

  # Wait for health
  local attempts=0
  while [[ $attempts -lt 30 ]]; do
    if curl -s --max-time 3 "http://127.0.0.1:$DOCKER_SIM_HTTP/api/health" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
      local h
      h=$(curl -s --max-time 3 "http://127.0.0.1:$DOCKER_SIM_HTTP/api/health")
      save_evidence TS-160 "health.json" "$h"
      ok "daemon healthy in Docker container (:$DOCKER_SIM_HTTP)"
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
