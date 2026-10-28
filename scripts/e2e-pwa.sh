#!/usr/bin/env bash
# e2e-pwa.sh — run a PWA story (default: schedule-flood) against an isolated
# local daemon. No Docker required.
#
# Usage:
#   bash scripts/e2e-pwa.sh [story-base-name]        # default: schedule-flood
#   bash scripts/e2e-pwa.sh load | group:load        # the `load` group (TS-802..TS-812)
#   TEST_TRUSTED_TLS=https://127.0.0.1:2443 bash scripts/e2e-pwa.sh
#
# What it does (local mode):
#   1. Picks free 127.0.0.1 ports and generates an isolated daemon config from
#      testdata/datawatch.yaml (same mechanism as scripts/run-tests.sh),
#   2. starts the daemon (bin/datawatch, TEST_BINARY, PATH, or `go build`),
#   3. `npm ci` in scripts/test-stories/pwa,
#   4. runs `node <story>.mjs` with TEST_HTTP / TEST_TLS / TEST_TOKEN (and
#      TEST_TRUSTED_TLS when set) — or, for a group argument (e.g. `load`),
#      `node run-all.mjs --group load` instead of a single story file,
#   5. stops the daemon; the per-run dir (evidence + daemon log) is kept on
#      non-zero exit.
#
# External-daemon mode: if TEST_HTTP, TEST_TLS and TEST_TOKEN are all set, the
# already-running daemon at those origins is used and no daemon is started.
#
# Exit code: single story — the story's own exit code (0 pass · 1 fail · 2
# skip). Group — run-all.mjs's exit code (0 pass, 1 fail; stories that exit
# 2 are counted as skipped and do not fail the run).

set -u

ARG="${1:-schedule-flood}"
GROUP=""
case "$ARG" in
  group:*) GROUP="${ARG#group:}" ;;
esac
if [[ "$ARG" == "load" ]]; then GROUP="load"; fi
STORY="${GROUP:-$ARG}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
PWA_DIR="$REPO_DIR/scripts/test-stories/pwa"
STORY_FILE="$PWA_DIR/${STORY}.mjs"
EVIDENCE_DIR=""
DAEMON_PID=""
RUN_DIR=""

fail() { echo "e2e-pwa: $*" >&2; exit 1; }

if [[ -z "$GROUP" ]]; then
  [[ -f "$STORY_FILE" ]] || fail "story not found: $STORY_FILE"
else
  ( cd "$PWA_DIR" && node run-all.mjs --dry-run --group "$GROUP" ) >/dev/null \
    || fail "unknown story group: $GROUP"
fi
command -v node >/dev/null 2>&1 || fail "node not found (install Node.js or add it to PATH)"
command -v npm  >/dev/null 2>&1 || fail "npm not found (install Node.js with npm)"

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null \
    || cat /proc/sys/kernel/random/uuid | head -c 8
}

# --- daemon lifecycle: scripts/test-daemon-lib.sh ------------------------
# The library owns the end path: it ends the daemon's sessions via the API
# first (recorded token), then the daemon by its recorded pid — only after
# its cmdline still carries the recorded marker (this run's config path).
# Processes are never matched by name.
TD_LIB="$SCRIPT_DIR/test-daemon-lib.sh"
[[ -f "$TD_LIB" ]] || fail "missing shared daemon lifecycle helper: $TD_LIB"
# shellcheck source=test-daemon-lib.sh
. "$TD_LIB"

cleanup() {
  local rc=$?
  # td_stop_all covers the daemon this script started (local mode). It is a
  # no-op in external-daemon mode (nothing recorded) and after the daemon
  # already ended.
  td_stop_all 2>/dev/null || true
  if [[ -n "$RUN_DIR" && -d "$RUN_DIR" ]]; then
    if [[ $rc -eq 0 ]]; then
      rm -rf "$RUN_DIR" 2>/dev/null || true
    else
      echo "e2e-pwa: run failed (exit $rc) — keeping evidence + daemon log in: $RUN_DIR" >&2
    fi
  fi
}
# All four paths are trapped: a signal the shell does not trap kills the
# shell without running its EXIT trap, leaving the daemon (and its sessions)
# behind. The traps are installed before the daemon is started.
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

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
  # td_start records the pid with the config path as the cmdline marker, so
  # every exit path (EXIT/INT/TERM/HUP) ends exactly this daemon — verified
  # against its cmdline, never by name.
  if ! DAEMON_PID="$(td_start "$BINARY" "$RUN_DIR/config.yaml" "$RUN_DIR/daemon.log")"; then
    tail -20 "$RUN_DIR/daemon.log" 2>/dev/null >&2 || true
    fail "failed to start the daemon (see $RUN_DIR/daemon.log)"
  fi

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
  # Record base URL + token for the exit path's API session pass.
  td_set_api "$DAEMON_PID" "$TEST_HTTP" "$TOKEN" \
    || echo "e2e-pwa: warn: could not record API credentials; exit path ends the daemon by pid only" >&2
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

export TEST_HTTP TEST_TLS TEST_TOKEN="${TEST_TOKEN:-dw-e2e-pwa-token}" EVIDENCE_DIR
[[ -n "${TEST_TRUSTED_TLS:-}" ]] && export TEST_TRUSTED_TLS

if [[ -n "$GROUP" ]]; then
  echo "e2e-pwa: running node run-all.mjs --group $GROUP (evidence: $EVIDENCE_DIR/$STORY)"
  ( cd "$PWA_DIR" && node run-all.mjs --group "$GROUP" )
  rc=$?
else
  echo "e2e-pwa: running node ${STORY}.mjs (evidence: $EVIDENCE_DIR/$STORY)"
  export CURRENT_STORY="$STORY"
  ( cd "$PWA_DIR" && node "${STORY}.mjs" )
  rc=$?
fi
if [[ $rc -ne 0 && $rc -ne 2 ]]; then
  echo "e2e-pwa: $STORY FAILED (exit $rc) — see $EVIDENCE_DIR/$STORY" >&2
fi
exit $rc
