#!/bin/sh
# datawatch container entrypoint
#
# Responsibilities:
#   1. If /data/config.yaml is missing AND we're not in bootstrap mode,
#      materialize a minimal default so `datawatch start` can boot.
#   2. If DATAWATCH_BOOTSTRAP_URL is set (Sprint 3 wiring), let the daemon
#      pull its real config from the parent — entrypoint stays out of the way.
#   3. Honor DATAWATCH_WORKSPACE_ROOT so relative project_dirs resolve under
#      the mounted volume (matches internal/config.SessionConfig.WorkspaceRoot).
#   4. Forward all CLI args to the datawatch binary (default: "start --foreground").
#
# Kept POSIX-sh so it runs identically on debian-slim and any future
# alpine/distroless variants.

set -eu

DATA_DIR="${DATAWATCH_DATA_DIR:-/data}"
CONFIG_FILE="${DATA_DIR}/config.yaml"
WORKSPACE_ROOT="${DATAWATCH_WORKSPACE_ROOT:-/workspace}"

# In bootstrap mode the daemon will rewrite config from the parent's response;
# we don't need to seed anything here. Sprint 3+ behavior.
if [ -n "${DATAWATCH_BOOTSTRAP_URL:-}" ]; then
    echo "[entrypoint] bootstrap mode: parent=${DATAWATCH_BOOTSTRAP_URL}, deferring config to daemon"
    exec /usr/local/bin/datawatch "$@"
fi

# Standalone mode: ensure a minimal config exists so the daemon can boot
# without further interaction. Operators bake their real config in via
# volume mount or kube ConfigMap.
if [ ! -f "${CONFIG_FILE}" ]; then
    echo "[entrypoint] no ${CONFIG_FILE} found, writing minimal default"
    mkdir -p "${DATA_DIR}"
    cat > "${CONFIG_FILE}" <<EOF
session:
  workspace_root: "${WORKSPACE_ROOT}"
  llm_backend: "shell"
  max_sessions: 10
server:
  host: "0.0.0.0"
  port: 8080
  enabled: true
  tls_enabled: false
mcp:
  enabled: true
  sse_enabled: false
memory:
  enabled: false
EOF
fi

exec /usr/local/bin/datawatch "$@"
