#!/usr/bin/env bash
# e2e-pwa.sh — run a PWA story (default: schedule-flood) against an isolated
# local daemon. No Docker required.
#
# Usage:
#   bash scripts/e2e-pwa.sh [story-base-name]        # default: schedule-flood
#   TEST_TRUSTED_TLS=https://127.0.0.1:2443 bash scripts/e2e-pwa.sh
#
# What it does (local mode):
#   1. Picks free 127.0.0.1 ports and generates an isolated daemon config from
#      testdata/datawatch.yaml (same mechanism as scripts/run-tests.sh),
#   2. starts the daemon (bin/datawatch, TEST_BINARY, PATH, or `go build`),
#   3. `npm ci` in scripts/test-stories/pwa,
#   4. runs `node <story>.mjs` with TEST_HTTP / TEST_TLS / TEST_TOKEN (and
#      TEST_TRUSTED_TLS when set),
#   5. stops the daemon; the per-run dir (evidence + daemon log) is kept on
#      non-zero exit.
#
# External-daemon mode: if TEST_HTTP, TEST_TLS and TEST_TOKEN are all set, the
# already-running daemon at those origins is used and no daemon is started.
#
# Exit code: the story's own exit code — 0 pass · 1 fail · 2 skip.

set -u

STORY="${1:-schedule-flood}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
PWA_DIR="$REPO_DIR/scripts/test-stories/pwa"
STORY_FILE="$PWA_DIR/${STORY}.mjs"
EVIDENCE_DIR=""
DAEMON_PID=""
RUN_DIR=""

fail() { echo "e2e-pwa: $*" >&2; exit 1; }

[[ -f "$STORY_FILE" ]] || fail "story not found: $STORY_FILE"
command -v node >/dev/null 2>&1 || fail "node not found (install Node.js or add it to PATH)"
command -v npm  >/dev/null 2>&1 || fail "npm not found (install Node.js with npm)"

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null \
    || cat /proc/sys/kernel/random/uuid | head -c 8
}

cleanup() {
  local rc=$?
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  if [[ -n "$RUN_DIR" && -d "$RUN_DIR" ]]; then
    if [[ $rc -eq 0 ]]; then
      rm -rf "$RUN_DIR" 2>/dev/null || true
    else
      echo "e2e-pwa: run failed (exit $rc) — keeping evidence + daemon log in: $RUN_DIR" >&2
    fi
  fi
}
trap cleanup EXIT

# --- daemon ------------------------------------------------------------------
if [[ -n "${TEST_HTTP:-}" && -n "${TEST_TLS:-}" && -n "${TEST_TOKEN:-}" ]]; then
  echo "e2e-pwa: external daemon mode (TEST_HTTP=$TEST_HTTP TEST_TLS=$TEST_TLS)"
else
  TOKEN="${TEST_TOKEN:-dw-e2e-pwa-token}"
  TEST_PORT="$(free_port)"
  TEST_TLS_PORT="$(free_port)"
  TEST_MCP_PORT="$(free_port)"
  TEST_HTTP="http://127.0.0.1:$TEST_PORT"
  TEST_TLS="https://127.0.0.1:$TEST_TLS_PORT"
  RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/dw-e2e-pwa-XXXXXX")"

  TMPL="$REPO_DIR/testdata/datawatch.yaml"
  [[ -f "$TMPL" ]] || fail "config template not found: $TMPL"
  sed \
    -e "s|data_dir: /data|data_dir: $RUN_DIR/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $RUN_DIR/data/projects|g" \
    -e "s|port: 8080|port: $TEST_PORT|g" \
    -e "s|tls_port: 8443|tls_port: $TEST_TLS_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $TEST_MCP_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"${TOKEN}\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    "$TMPL" > "$RUN_DIR/config.yaml"

  BINARY="${TEST_BINARY:-}"
  [[ -z "$BINARY" && -x "$REPO_DIR/bin/datawatch" ]] && BINARY="$REPO_DIR/bin/datawatch"
  [[ -z "$BINARY" ]] && BINARY="$(command -v datawatch 2>/dev/null || true)"
  if [[ -z "$BINARY" ]]; then
    command -v go >/dev/null 2>&1 \
      && (cd "$REPO_DIR" && go build -o bin/datawatch ./cmd/datawatch) \
      || fail "datawatch binary not found (set TEST_BINARY, pre-build, or install Go)"
    BINARY="$REPO_DIR/bin/datawatch"
  fi

  mkdir -p "$RUN_DIR/data"
  echo "e2e-pwa: starting daemon ($BINARY) http=$TEST_HTTP tls=$TEST_TLS"
  "$BINARY" start --foreground --config "$RUN_DIR/config.yaml" \
    > "$RUN_DIR/daemon.log" 2>&1 &
  DAEMON_PID=$!

  deadline=$(( $(date +%s) + 60 ))
  healthy=0
  while [[ $(date +%s) -lt $deadline ]]; do
    if curl -skf "$TEST_TLS/api/health" >/dev/null 2>&1 || \
       curl -sf  "$TEST_HTTP/api/health"  >/dev/null 2>&1; then
      healthy=1; break
    fi
    sleep 0.5
  done
  [[ $healthy -eq 1 ]] || {
    echo "--- last 20 lines of $RUN_DIR/daemon.log ---" >&2
    tail -20 "$RUN_DIR/daemon.log" >&2 || true
    fail "daemon did not become healthy within 60s"
  }
  echo "e2e-pwa: daemon healthy (PID $DAEMON_PID)"
fi

# --- story -------------------------------------------------------------------
if [[ -n "${RUN_DIR:-}" ]]; then
  EVIDENCE_DIR="${EVIDENCE_DIR:-$RUN_DIR/evidence}"   # local-daemon mode
else
  EVIDENCE_DIR="${EVIDENCE_DIR:-/tmp/dw-e2e-pwa-evidence}"  # external-daemon mode
fi
mkdir -p "$EVIDENCE_DIR/$STORY"

if [[ -f "$PWA_DIR/package-lock.json" ]]; then
  ( cd "$PWA_DIR" && npm ci --no-audit --no-fund --loglevel=error ) \
    || fail "npm ci failed in $PWA_DIR"
else
  ( cd "$PWA_DIR" && npm install --no-audit --no-fund --loglevel=error ) \
    || fail "npm install failed in $PWA_DIR"
fi

echo "e2e-pwa: running node ${STORY}.mjs (evidence: $EVIDENCE_DIR/$STORY)"
export TEST_HTTP TEST_TLS TEST_TOKEN="${TEST_TOKEN:-dw-e2e-pwa-token}" EVIDENCE_DIR
export CURRENT_STORY="$STORY"
[[ -n "${TEST_TRUSTED_TLS:-}" ]] && export TEST_TRUSTED_TLS

( cd "$PWA_DIR" && node "${STORY}.mjs" )
rc=$?
if [[ $rc -ne 0 && $rc -ne 2 ]]; then
  echo "e2e-pwa: story $STORY FAILED (exit $rc) — see $EVIDENCE_DIR/$STORY" >&2
fi
exit $rc
