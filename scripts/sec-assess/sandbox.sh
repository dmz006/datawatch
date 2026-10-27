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
#   ./sandbox.sh wipe                                          # end it + remove
#
# `start` stays in the foreground for as long as the sandbox runs (run it as
# a background job to use the sandbox from the same shell): the daemon is
# started and recorded through scripts/test-daemon-lib.sh, so interrupting
# `start`, a failed start, a signal, or the lifetime limit below ends the
# daemon (its sessions first, through its own API) on every exit path.
#
# Env overrides: DW_SANDBOX_DIR, DW_BIN, DW_BIND, DW_PORT, DW_TLS_PORT,
#   DW_SANDBOX_TIMEOUT (maximum lifetime of `start` in seconds, default 3600)
#
# SAFETY: refuses to run on the default production ports (8080/8443),
# refuses to reuse an existing non-sandbox data dir, and `wipe` ends the
# daemon recorded for this sandbox (by its recorded PID, after checking its
# live command line still carries this sandbox's config path), then removes
# $DW_SANDBOX_DIR.

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
SUPF="$STATE/supervisor.pid"
LIFETIME="${DW_SANDBOX_TIMEOUT:-3600}"
# Daemon records of this sandbox live inside its own state directory, so a
# later `wipe` reads the same records `start` wrote.
export TD_STATE_DIR="$STATE/daemon-state"
ARGS=("$@")

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

# sandbox_forget_state — drop the pid/url files once the daemon is gone.
sandbox_forget_state() {
  rm -f "$PIDF" "$URLF" "$SUPF"
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
      # Checked before the lifecycle traps exist: this run recorded nothing,
      # so it must not end the daemon another `start` recorded.
      echo "sandbox already running (pid $(cat "$PIDF")) — $(cat "$URLF" 2>/dev/null || echo 'url unknown')"
      exit 0
    fi
    mkdir -p "$STATE" "$SB/work"
    # Lifecycle first, before any daemon exists: the traps installed here end
    # the recorded daemon on every exit path of this command.
    # shellcheck source=../test-daemon-lib.sh
    source "$ROOT_DIR/scripts/test-daemon-lib.sh"
    TD_EXTRA_CLEANUP=sandbox_forget_state
    td_install_traps "${ARGS[@]}"
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
    : > "$LOGF"
    DATAWATCH_DATA_DIR="$SB" td_start "$BIN" "$SB/config.yaml" "$LOGF"
    SANDBOX_PID="$TD_LAST_PID"
    echo "$SANDBOX_PID" > "$PIDF"
    echo $$ > "$SUPF"
    td_set_api "$SANDBOX_PID" "https://127.0.0.1:$TLSPORT" "$TOKEN"
    wait_health
    echo "✓ sandbox up: $(cat "$URLF")"
    echo "  pid=$SANDBOX_PID  dir=$SB  log=$LOGF"
    echo "  base_url=$(cat "$URLF")"
    echo "  staying in the foreground; interrupt (or \`$0 wipe\`) ends it, at most ${LIFETIME}s"
    # Stay alive for as long as the daemon runs, up to the lifetime limit; the
    # traps end the daemon when this loop is left or this process is signalled.
    lifetime_end=$((SECONDS + LIFETIME))
    while td_alive "$SANDBOX_PID"; do
      if ((SECONDS >= lifetime_end)); then
        echo "sandbox lifetime limit (${LIFETIME}s) reached; ending it"
        break
      fi
      sleep 1
    done
    ;;

  url)
    [[ -f "$URLF" ]] && cat "$URLF" || { echo "no running sandbox (no $URLF)" >&2; exit 1; }
    ;;

  wipe)
    # shellcheck source=../test-daemon-lib.sh
    source "$ROOT_DIR/scripts/test-daemon-lib.sh"
    # First the `start` process that supervises the daemon: its traps end the
    # daemon, sessions first. Only the PID `start` recorded, and only while
    # its live command line still names this script.
    if [[ -f "$SUPF" ]]; then
      sup="$(cat "$SUPF")"
      if [[ "$sup" =~ ^[0-9]+$ ]] && kill -0 "$sup" 2>/dev/null \
          && tr '\0' ' ' < "/proc/$sup/cmdline" 2>/dev/null | grep -q 'sandbox\.sh'; then
        echo "→ ending sandbox supervisor pid $sup"
        kill -TERM "$sup" 2>/dev/null || true
        for _ in $(seq 1 20); do
          kill -0 "$sup" 2>/dev/null || break
          sleep 1
        done
      fi
    fi
    # Then whatever `start` recorded and could not end itself (a supervisor
    # that was killed outright leaves its records behind).
    echo "→ ending recorded sandbox daemon"
    td_stop_all
    if [[ -f "$PIDF" ]]; then
      pid="$(cat "$PIDF")"
      # Still running only if the live command line still carries this
      # sandbox's config path (a recycled pid is somebody else's process).
      if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null \
          && tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -qF "$SB/config.yaml"; then
        echo "sandbox daemon pid $pid is still running; not removing $SB" >&2
        exit 1
      fi
    fi
    rm -f "$PIDF" "$URLF" "$SUPF"
    echo "→ removing $SB"
    rm -rf "$SB"
    echo "✓ sandbox wiped"
    ;;

  *)
    echo "usage: $0 {start [--token tok] [--bind host] [--port n] [--tls-port n] | url | wipe}" >&2
    exit 2
    ;;
esac
