#!/usr/bin/env bash
#
# test-daemon-lib.sh — shared daemon-lifecycle helper for test stories.
#
# Source this at the top of a test script that starts a sandbox `datawatch`
# daemon.  It provides:
#
#   td_start daemon_port         start `datawatch start --foreground` on a
#                                port, in the foreground so the caller can
#                                trap it; never backgrounds the daemon
#   td_track_pid PID marker      record a daemon pid plus a command-line marker
#   td_set_api base token tmo    record the test token / base url used to kill
#                                API sessions on shutdown
#   td_stop_one PID              stop one recorded daemon, killing its sessions
#                                through the API first, then TERM -> KILL
#   td_stop_all                  stop every recorded daemon
#   td_install_traps             install EXIT/INT/TERM/HUP traps that preserve
#                                the exit status and always stop everything
#   td_wait_port_closed h p secs wait until a listening port has closed
#
# Safety rules baked in:
#   * Only pids the script itself recorded are ever signalled, and every kill
#     is re-verified against /proc/<pid>/cmdline (the recorded marker) right
#     before it fires, so a pid recycled to another process is never hit.
#   * Nothing is killed or looked up by process name.
#   * The cleanup runs under a plain pid kill, not through the service manager.
#   * The test runner launches these fixtures with `env -u TMUX -u TMUX_TMPDIR`
#     so no signal is forwarded to a live tmux server.
#
# Signal model:
#   TERM / HUP run a handler (these are not ignored when a shell starts), stop
#   everything, and exit with 143 / 129.  EXIT preserves the original status and
#   also stops everything.  INT is the one signal a background-launched fixture
#   (SIGINT ignored on entry) cannot attach a handler to, so we reset INT to its
#   default disposition (`trap - INT`): a `kill -INT` then ends the script 130
#   with no handler, and a detached watchdog child — which outlives the shell —
#   performs the shutdown so the daemon is still reaped.

# ---------------------------------------------------------------------------
# state
# ---------------------------------------------------------------------------

TD_STATE_DIR="${TD_STATE_DIR:-}"
TD_API_BASE=""
TD_API_TOKEN=""
TD_API_TIMEOUT="5"

# Ensure the per-run state directory exists and is exported (so the watchdog
# child and any subshells see the same one).
_td_state_dir() {
  if [[ -z "${TD_STATE_DIR:-}" ]]; then
    TD_STATE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/tdl.XXXXXX")"
  fi
  mkdir -p "$TD_STATE_DIR"
  export TD_STATE_DIR
}

# Read a single `key=value` line from a state file, stripping one layer of
# surrounding single quotes.  Safe for arbitrary values (no eval, no word split).
_td_read_state() {
  local state="$1" key="$2" line found=""
  [[ -f "$state" ]] || return 0
  while IFS= read -r line; do
    if [[ "$line" == "${key}="* ]]; then
      line="${line#*=}"
      if [[ "${#line}" -ge 2 && "${line:0:1}" == "'" && "${line: -1}" == "'" ]]; then
        line="${line:1:${#line}-2}"
      fi
      found="$line"
      break
    fi
  done < "$state"
  printf '%s' "$found"
}

# ---------------------------------------------------------------------------
# td_set_api — record the test token / base url for session kills
# ---------------------------------------------------------------------------
td_set_api() {
  local base="${1:-}" token="${2:-}" tmo="${3:-}"
  _td_state_dir
  [[ -n "$tmo" ]] && TD_API_TIMEOUT="$tmo"
  export TD_API_BASE="$base"
  export TD_API_TOKEN="$token"
  export TD_API_TIMEOUT="$TD_API_TIMEOUT"
  # Record it in the state dir so the watchdog child and td_stop_one (which
  # may run in a subshell) can read it at shutdown time, regardless of when the
  # caller set it.
  {
    printf "api_base=%s\n" "'$base'"
    printf "api_token=%s\n" "'$token'"
    printf "api_timeout=%s\n" "'$TD_API_TIMEOUT'"
  } > "$TD_STATE_DIR/api.env" 2>/dev/null
  return 0
}

# End every session the recorded daemon is serving, through the datawatch REST
# API, before the daemon is signalled.  Reads the api config from the state dir
# (if the caller set it), lists sessions, and kills each.  No-op when no api is
# configured or when curl / the endpoint are unavailable (test fixtures have no
# server and never call td_set_api).
_td_end_sessions_via_api() {
  local base="${TD_API_BASE:-}" token="${TD_API_TOKEN:-}" tmo="${TD_API_TIMEOUT:-5}"
  [[ -n "$base" ]] || return 0
  command -v curl >/dev/null 2>&1 || return 0
  command -v python3 >/dev/null 2>&1 || return 0
  base="${base%/}"
  local list ids id
  list="$(curl -s --max-time "$tmo" -H "Authorization: Bearer ${token}" \
      "$base/api/sessions" 2>/dev/null)" || return 0
  [[ -n "$list" ]] || return 0
  # GET /api/sessions is a JSON array of session objects (each has full_id / id).
  ids="$(printf '%s' "$list" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
out = []
def add(o):
    if isinstance(o, dict):
        v = o.get("full_id") or o.get("id")
        if v:
            out.append(str(v))
if isinstance(d, list):
    for i in d:
        add(i)
elif isinstance(d, dict):
    for k in ("sessions", "data", "items"):
        v = d.get(k)
        if isinstance(v, list):
            for i in v:
                add(i)
    add(d)
sys.stdout.write("\n".join(out))
' 2>/dev/null)" || return 0
  [[ -n "$ids" ]] || return 0
  printf '%s\n' "$ids" | while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    curl -s --max-time "$tmo" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json" \
      -X POST -d "{\"id\":\"$id\"}" \
      "$base/api/sessions/kill" >/dev/null 2>&1
  done
  return 0
}

# ---------------------------------------------------------------------------
# td_track_pid — record a daemon pid and its command-line marker
# ---------------------------------------------------------------------------
td_track_pid() {
  local pid="${1:-}" marker="${2:-}"
  _td_state_dir
  local state="$TD_STATE_DIR/$pid"
  {
    printf "pid=%s\n" "'$pid'"
    printf "marker=%s\n" "'$marker'"
  } > "$state" 2>/dev/null
  return 0
}

# ---------------------------------------------------------------------------
# td_stop_one — stop one recorded daemon (API sessions first, then TERM/KILL)
# ---------------------------------------------------------------------------
td_stop_one() {
  local pid="${1:-}"
  [[ -n "$pid" ]] || return 0
  _td_state_dir
  local state="$TD_STATE_DIR/$pid"
  local marker
  marker="$(_td_read_state "$state" marker)"

  # Re-verify identity from /proc/<pid>/cmdline before touching anything, so a
  # recycled pid that no longer matches the recorded marker is never signalled.
  local cmdline=""
  if [[ -r "/proc/$pid/cmdline" ]]; then
    cmdline="$(tr "\0" " " < "/proc/$pid/cmdline" 2>/dev/null || true)"
    cmdline="${cmdline% }"
  fi
  if [[ -n "$marker" ]]; then
    case "$cmdline" in
      *"$marker"*) ;;            # still the daemon we recorded
      *) rm -f "$state" 2>/dev/null; return 0 ;;   # gone or recycled: do not signal
    esac
  fi

  # Kill the daemon's API sessions first (the daemon is still alive, serving).
  _td_end_sessions_via_api

  # Graceful, then forced — by recorded pid only, never by name.
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    local t
    for t in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.05 2>/dev/null || :
    done
  fi
  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null || true
    for t in 1 2 3 4 5 6 7 8; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.05 2>/dev/null || :
    done
  fi
  rm -f "$state" 2>/dev/null
  return 0
}

# ---------------------------------------------------------------------------
# td_stop_all — stop every recorded daemon
# ---------------------------------------------------------------------------
td_stop_all() {
  _td_state_dir
  local state pid
  for state in "$TD_STATE_DIR"/*; do
    [[ -f "$state" ]] || continue
    pid="${state##*/}"
    td_stop_one "$pid"
  done
  return 0
}

# ---------------------------------------------------------------------------
# td_wait_port_closed — wait until a host:port stops accepting connections
# ---------------------------------------------------------------------------
td_wait_port_closed() {
  local host="${1:-127.0.0.1}" port="${2:-}" tmo="${3:-10}"
  [[ -n "$port" ]] || return 0
  local deadline=$(( $(date +%s 2>/dev/null || echo 0) + tmo ))
  while (( $(date +%s 2>/dev/null || echo 0) < deadline )); do
    # A successful connect means the port is still open: keep waiting.
    if bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null; then
      sleep 0.1 2>/dev/null || :
      continue
    fi
    # Connect refused: no longer listening.
    return 0
  done
  return 0
}

# ---------------------------------------------------------------------------
# td_start — start a sandbox daemon in the foreground on a given port
# ---------------------------------------------------------------------------
td_start() {
  local port="${1:-}"
  local bin="${TD_DAEMON_BIN:-datawatch}"
  local cfg="${TD_CONFIG:-}"
  local max="${TD_START_MAX_ATTEMPTS:-50}"
  local attempt=0 rc=1
  local upmarker
  upmarker="$(mktemp "${TMPDIR:-/tmp}/tdl-up.XXXX" 2>/dev/null)" || upmarker="/dev/null/tdl-up"
  while (( attempt < max )); do
    attempt=$((attempt + 1))
    rm -f "$upmarker" 2>/dev/null
    # A background watcher (NOT the daemon) records when the port starts
    # accepting; the daemon itself runs in the foreground below so the caller
    # can trap it and we never background the daemon.
    (
      while :; do
        if bash -c "exec 3<>/dev/tcp/127.0.0.1/$port" 2>/dev/null; then
          : 1 > "$upmarker"
          break
        fi
        sleep 0.2 2>/dev/null || :
      done
    ) &
    local w=$!
    "$bin" start --foreground --config "$cfg" \
      --server.host 127.0.0.1 --server.port "$port"
    rc=$?
    wait "$w" 2>/dev/null || true
    if [[ -s "$upmarker" ]]; then
      # The daemon came up and then exited (port closed) -> success.
      rm -f "$upmarker" 2>/dev/null
      return 0
    fi
    # The daemon exited before the port ever opened (startup crash) -> restart.
    rm -f "$upmarker" 2>/dev/null
  done
  rm -f "$upmarker" 2>/dev/null
  return "$rc"
}

# ---------------------------------------------------------------------------
# td_install_traps — EXIT / INT / TERM / HUP traps that always clean up
# ---------------------------------------------------------------------------

# Run the caller's optional extra cleanup (TD_EXTRA_CLEANUP) last.
_td_extra_cleanup() {
  if [[ -n "${TD_EXTRA_CLEANUP:-}" ]]; then
    bash -c "$TD_EXTRA_CLEANUP" || true
  fi
  return 0
}

# Detached watchdog child.  A background job inherits SIGINT ignored on entry,
# so in the INT case no handler can run in the shell itself; this child, which
# survives the shell's death, performs the shutdown instead.  It also covers
# TERM/HUP/EXIT for robustness and is idempotent with those paths.
_td_watchdog() {
  local spinned=0
  while kill -0 "$$" 2>/dev/null; do
    sleep 0.2 2>/dev/null || sleep 1 2>/dev/null || :
    spinned=$((spinned + 1))
    if (( spinned > 20000 )); then break; fi   # defensive: never run forever
  done
  td_stop_all >/dev/null 2>&1 || true
  return 0
}

td_install_traps() {
  _td_state_dir

  # EXIT: preserve the original status, stop everything, then run the caller's
  # optional TD_EXTRA_CLEANUP last.
  trap 'rc=$?; td_stop_all; _td_extra_cleanup; exit "$rc"' EXIT

  # TERM and HUP: allowed to have real handlers (not ignored on entry).  Stop
  # everything and exit with the conventional 128+signum status.
  trap 'td_stop_all; exit 143' TERM
  trap 'td_stop_all; exit 129' HUP

  # INT: a background-launched fixture cannot attach an INT handler (SIGINT is
  # ignored on entry, and bash will not trap a signal ignored on entry).  Reset
  # INT to its default disposition instead: `kill -INT` then ends the script 130
  # with no handler, and the watchdog (below) reaps the daemon.  Harmless when
  # INT is already default (interactive / direct runs).
  trap - INT 2>/dev/null || true

  # Watchdog child: outlives the shell, performs shutdown in the INT case and
  # backstops TERM/HUP/EXIT.
  ( _td_watchdog ) & disown 2>/dev/null || true

  return 0
}

# A library: do not treat the end of file as an error for a sourced file.
# (Nothing to run on source; the caller invokes the td_* functions above.)
return 0 2>/dev/null
true
