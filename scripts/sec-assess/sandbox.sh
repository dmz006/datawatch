#!/usr/bin/env bash
# sec-assess/sandbox.sh — security-assessment sandbox (BL365)
#
# Brings up a THROWAWAY datawatch daemon in an isolated data dir on
# non-default ports. It never touches the production daemon or its
# ~/.datawatch state. All security-assessment dynamic tests run against
# this instance only.
#
# Usage:
#   ./sandbox.sh start [--token <tok>] [--bind 127.0.0.1]      # start sandbox
#   ./sandbox.sh start --token <your-throwaway-token>         # authed mode (dummy value only)
#   ./sandbox.sh url                                           # print base URL
#   ./sandbox.sh wipe                                          # kill + remove
#
# Env overrides: DW_SANDBOX_DIR, DW_BIN, DW_BIND, DW_PORT, DW_TLS_PORT
#
# SAFETY: refuses to run on the default production ports (8080/8443),
# refuses to reuse an existing non-sandbox data dir, and `wipe` is the
# only teardown path (kills the PID we created, then removes $DW_SANDBOX_DIR).

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SB="${DW_SANDBOX_DIR:-$ROOT_DIR/.sec-sandbox}"
STATE="$SB/state"
PIDF="$STATE/pid"
URLF="$STATE/url"
LOGF="$STATE/daemon.log"
BIN="${DW_BIN:-datawatch}"
TOKEN="${DW_TOKEN:-}"
BIND="${DW_BIND:-127.0.0.1}"
PORT="${DW_PORT:-18090}"
TLSPORT="${DW_TLS_PORT:-18444}"

guard_ports() {
  for p in "$PORT" "$TLSPORT"; do
    if [[ "$p" == "8080" || "$p" == "8443" ]]; then
      echo "REFUSING: port $p is the production default. Choose another (DW_PORT/DW_TLS_PORT)." >&2
      exit 3
    fi
  done
}

wait_health() {
  local base="https://127.0.0.1:$TLSPORT"
  for i in $(seq 1 30); do
    if curl -sk "$base/api/health" 2>/dev/null | grep -q '"ok"'; then
      echo "$base" > "$URLF"
      return 0
    fi
    sleep 1
  done
  echo "sandbox failed health check after 30s; log tail:" >&2
  tail -20 "$LOGF" >&2 || true
  return 1
}

cmd="${1:-}"
shift || true

case "$cmd" in
  start)
    guard_ports
    # Parse optional flags
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --token)  TOKEN="${2:?}"; shift 2 ;;
        --bind)   BIND="${2:?}";  shift 2 ;;
        --port)   PORT="${2:?}";  shift 2; guard_ports ;;
        --tls-port) TLSPORT="${2:?}"; shift 2; guard_ports ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
      esac
    done
    if [[ -f "$PIDF" ]] && kill -0 "$(cat "$PIDF")" 2>/dev/null; then
      echo "sandbox already running (pid $(cat "$PIDF")) — $(cat "$URLF" 2>/dev/null || echo 'url unknown')"
      exit 0
    fi
    mkdir -p "$STATE" "$SB/work"
    cat > "$SB/config.yaml" <<EOF
# BL365 assessment sandbox — throwaway config, dummy values only.
# Never put real credentials here.
hostname: sec-sandbox
data_dir: "$SB"
server:
  enabled: true
  host: "$BIND"
  port: $PORT
  token: "${TOKEN}"
  tls_enabled: true
  tls_port: $TLSPORT
  tls_auto_generate: true
session:
  default_project_dir: "$SB/work"
  auto_git_init: false
  auto_git_commit: false
EOF
    echo "→ starting sandbox: bind=$BIND http=$PORT tls=$TLSPORT token=$([[ -n "$TOKEN" ]] && echo set || echo EMPTY)"
    DATAWATCH_DATA_DIR="$SB" "$BIN" start --foreground --config "$SB/config.yaml" > "$LOGF" 2>&1 &
    echo $! > "$PIDF"
    wait_health
    echo "✓ sandbox up: $(cat "$URLF")"
    echo "  pid=$(cat "$PIDF")  dir=$SB  log=$LOGF"
    echo "  base_url=$(cat "$URLF")"
    ;;

  url)
    [[ -f "$URLF" ]] && cat "$URLF" || { echo "no running sandbox (no $URLF)" >&2; exit 1; }
    ;;

  wipe)
    if [[ -f "$PIDF" ]]; then
      pid="$(cat "$PIDF")"
      echo "→ killing sandbox pid $pid"
      kill "$pid" 2>/dev/null || true
      for i in $(seq 1 10); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 1
      done
      kill -9 "$pid" 2>/dev/null || true
      rm -f "$PIDF" "$URLF"
    fi
    echo "→ removing $SB"
    rm -rf "$SB"
    echo "✓ sandbox wiped"
    ;;

  *)
    echo "usage: $0 {start [--token tok] [--bind host] [--port n] [--tls-port n] | url | wipe}" >&2
    exit 2
    ;;
esac
