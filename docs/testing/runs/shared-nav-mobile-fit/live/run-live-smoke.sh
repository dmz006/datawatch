#!/usr/bin/env bash
# run-live-smoke.sh — start an isolated sandbox daemon, run live-smoke.mjs against it,
# then stop only the daemon this script started (by PID).
#
# Usage (from the repo root):
#   env -u TMUX -u TMUX_TMPDIR bash docs/testing/runs/shared-nav-mobile-fit/live/run-live-smoke.sh
#
# The daemon config comes from testdata/datawatch.yaml with the same sed rewrite that
# scripts/e2e-pwa.sh uses (free 127.0.0.1 ports, sandbox data dir). The Chrome profile
# lives under bin/ (gitignored) and is removed afterwards.

set -u

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/../../../../.." && pwd)
TOKEN="${TEST_TOKEN:-dw-e2e-pwa-token}"
PROFILE="$REPO_DIR/bin/live-smoke-profile"
DAEMON_PID=""
RUN_DIR=""

fail() { echo "run-live-smoke: $*" >&2; exit 1; }

cleanup() {
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
    echo "run-live-smoke: stopped sandbox daemon PID $DAEMON_PID"
  fi
  [[ -n "$RUN_DIR" && -d "$RUN_DIR" ]] && rm -rf "$RUN_DIR"
  rm -rf "$PROFILE"
}
trap cleanup EXIT

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

[[ -x "$REPO_DIR/bin/datawatch" ]] || fail "bin/datawatch not built (run make build first)"
command -v node >/dev/null 2>&1 || fail "node not found"

PORT="$(free_port)"
TLS_PORT="$(free_port)"
MCP_PORT="$(free_port)"
TLS="https://127.0.0.1:$TLS_PORT"
RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/dw-live-smoke-XXXXXX")"

sed \
  -e "s|data_dir: /data|data_dir: $RUN_DIR/data|g" \
  -e "s|default_project_dir: /data/projects|default_project_dir: $RUN_DIR/data/projects|g" \
  -e "s|port: 8080|port: $PORT|g" \
  -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
  -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" \
  -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
  -e "s|token: \"\"|token: \"${TOKEN}\"|g" \
  -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
  "$REPO_DIR/testdata/datawatch.yaml" > "$RUN_DIR/config.yaml"
mkdir -p "$RUN_DIR/data"

echo "run-live-smoke: starting sandbox daemon (bin/datawatch) tls=$TLS"
env -u TMUX -u TMUX_TMPDIR "$REPO_DIR/bin/datawatch" start --foreground --config "$RUN_DIR/config.yaml" \
  > "$RUN_DIR/daemon.log" 2>&1 &
DAEMON_PID=$!

deadline=$(( $(date +%s) + 60 ))
healthy=0
while [[ $(date +%s) -lt $deadline ]]; do
  if curl -skf "$TLS/api/health" >/dev/null 2>&1; then healthy=1; break; fi
  sleep 0.5
done
if [[ $healthy -ne 1 ]]; then
  tail -20 "$RUN_DIR/daemon.log" >&2 || true
  fail "sandbox daemon did not become healthy within 60s"
fi
echo "run-live-smoke: sandbox daemon healthy (PID $DAEMON_PID)"

mkdir -p "$PROFILE"
TEST_TLS="$TLS" TEST_TOKEN="$TOKEN" OUT_DIR="$SCRIPT_DIR" CHROME_PROFILE="$PROFILE" \
  node "$SCRIPT_DIR/live-smoke.mjs"
exit $?
