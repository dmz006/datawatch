#!/usr/bin/env bash
# test-daemon-lib.sh — shared lifecycle helper for every script or story that
# starts a datawatch daemon for testing.
#
# Why this exists: daemons started by one-shot test runs have been found days
# later still holding ports and sessions because an earlier run died partway
# through cleanup (or interrupted mid-cleanup). The contract here is: a daemon
# is only ever ended by the exact pid the calling script recorded, and only
# after re-checking that the live /proc/<pid>/cmdline still carries the marker
# (the config path) recorded for that pid. Nothing in this file searches by
# name, never touches a multiplexer server, and every exit path of the
# calling script — normal, failed, INT, TERM, HUP — goes through td_stop_all.
#
# Usage:
#   source scripts/test-daemon-lib.sh
#   td_install_traps            # once, as early as possible, before the first td_start
#   td_start ./datawatch config.yaml daemon.log
#   td_set_api "$TD_LAST_PID" "http://127.0.0.1:18443" "$token"   # optional
#   td_wait_port_closed 18443 20                                    # optional
#
# State (one file per recorded pid) lives in TD_STATE_DIR so a story running
# in a subshell and the runner that owns the traps share the same records.

# ------------------------------------------------------------------ defaults:

# TD_STATE_DIR holds "<pid>" files (pid, marker, api_base, api_token). Default
# is a fresh private directory; a caller (or the verifier) can pre-set it so
# a subshell story and its runner share records.
TD_STATE_DIR="${TD_STATE_DIR:-$(mktemp -d 2>/dev/null)}"
export TD_STATE_DIR

# Seconds SIGTERM gets before td_stop_one escalates to SIGKILL.
export TD_STOP_TIMEOUT="${TD_STOP_TIMEOUT:-15}"

# ----------------------------------------------------------------- internals:

# _td_read_field FILE KEY — first value of a key= line; empty when absent.
_td_read_field() {
  local file="$1" key="$2"
  sed -n "s/^${key}=//p" "$file" 2>/dev/null | head -n 1
}

# _td_api_url BASE PATH — join, tolerating a trailing slash on BASE.
_td_api_url() {
  printf '%s%s' "${1%/}" "$2"
}

# _td_extract_ids BODY — print one session id per line. The sessions endpoint
# returns a bare JSON array of session objects (the server encodes the list
# directly); tolerate a dict wrapper with a "sessions" or "rows" list too.
# With python3 the body is passed via the environment (no stdin pipe, and the
# body is never re-quoted by bash); without python3 the "id" values are
# extracted textually — safe because session ids carry neither quotes nor
# backslashes, so a quote-free match cannot bleed into another field.
_td_extract_ids() {
  local body="$1"
  if command -v python3 >/dev/null 2>&1; then
    TD_BODY="$body" python3 -I -c 'import json, os
try:
    d = json.loads(os.environ.get("TD_BODY", ""))
except Exception:
    d = None
if isinstance(d, dict):
    d = d.get("sessions", d.get("rows"))
if isinstance(d, list):
    for s in d:
        if isinstance(s, dict):
            i = s.get("id")
            if isinstance(i, str) and i and "\n" not in i and "\r" not in i:
                print(i)
' 2>/dev/null
  else
    grep -oE '"id"[[:space:]]*:[[:space:]]*"[^"\\]*"' <<<"$body" \
      | sed -E 's/^"id"[[:space:]]*:[[:space:]]*"//; s/"$//'
  fi
  return 0
}

# _td_end_sessions_via_api BASE TOKEN — list the daemon's sessions and end each
# one by id through the daemon's own API before the daemon goes down.
# Short timeouts, every failure ignored: cleanup must never hang.
_td_end_sessions_via_api() {
  local base="$1" token="$2" body id ids
  command -v curl >/dev/null 2>&1 || return 0
  body=$(env -u TMUX -u TMUX_TMPDIR curl -sk --max-time 5 \
    -H "Authorization: Bearer $token" \
    "$(_td_api_url "$base" /api/sessions)" 2>/dev/null) || return 0
  [[ -n "$body" ]] || return 0
  ids=$(_td_extract_ids "$body") || ids=""
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    env -u TMUX -u TMUX_TMPDIR curl -sk --max-time 5 -X POST \
      -H "Authorization: Bearer $token" \
      -H 'Content-Type: application/json' \
      -d "{\"id\":\"$id\"}" \
      "$(_td_api_url "$base" /api/sessions/kill)" >/dev/null 2>&1 || true
  done <<<"$ids"
  return 0
}

# ------------------------------------------------------------------ recorded:

# td_track_pid PID MARKER — record an already-started pid. MARKER is the
# substring the live /proc/<PID>/cmdline must still contain before we signal.
td_track_pid() {
  local pid="$1" marker="${2-}"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  mkdir -p "$TD_STATE_DIR" 2>/dev/null || return 1
  {
    printf 'pid=%s\n' "$pid"
    printf 'marker=%s\n' "$marker"
    printf 'api_base=%s\n' ""
    printf 'api_token=%s\n' ""
  } > "$TD_STATE_DIR/$pid"
}

# td_set_api PID BASE_URL TOKEN — remember where this daemon's API listens, so
# its sessions can be ended through the API (POST /api/sessions/kill) before
# the daemon itself is signaled.
td_set_api() {
  local pid="$1" base="${2-}" token="${3-}"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  mkdir -p "$TD_STATE_DIR" 2>/dev/null || return 1
  local f="$TD_STATE_DIR/$pid" head
  if [[ -f "$f" ]]; then
    head=$(grep -vE '^(api_base|api_token)=' "$f" 2>/dev/null) || head=""
  else
    head="pid=$pid
marker="
  fi
  printf '%s\napi_base=%s\napi_token=%s\n' "$head" "$base" "$token" > "$f"
}

# ------------------------------------------------------------------- starting:

# td_start BINARY CONFIG LOGFILE — start "BINARY start --foreground
# --config CONFIG" in the background (tmux env stripped, appended to LOGFILE),
# record the pid with CONFIG as its cmdline marker, and set TD_LAST_PID.
td_start() {
  local binary="$1" config="${2-}" logfile="${3-}"
  [[ -n "$binary" && -n "$config" ]] || return 1
  [[ -n "$logfile" ]] || { logfile=/dev/null; }
  env -u TMUX -u TMUX_TMPDIR "$binary" start --foreground --config "$config" >>"$logfile" 2>&1 &
  TD_LAST_PID=$!
  export TD_LAST_PID
  td_track_pid "$TD_LAST_PID" "$config"
}

# -------------------------------------------------------------------- stopping:

# td_stop_one PID — end one recorded pid, in this order:
#   1. if an API address is recorded, list sessions and end each one by id;
#   2. re-verify the live /proc/<PID>/cmdline still carries the recorded
#      marker — refuse to signal a pid it no longer does (recycled pids);
#   3. SIGTERM, wait up to TD_STOP_TIMEOUT seconds, then SIGKILL, then reap.
td_stop_one() {
  local pid="$1"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  local f="${TD_STATE_DIR:?TD_STATE_DIR is not set}/$pid"
  [[ -f "$f" ]] || return 0

  local marker api_base api_token
  marker=$(_td_read_field "$f" marker)
  api_base=$(_td_read_field "$f" api_base)
  api_token=$(_td_read_field "$f" api_token)

  # (1) End the daemon's sessions through its own API, best effort.
  if [[ -n "$api_base" && -n "$api_token" ]]; then
    _td_end_sessions_via_api "$api_base" "$api_token" || true
  fi

  # Already gone: just drop the record.
  if ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$f"
    return 0
  fi

  # (2) Re-verify before signaling: the recorded marker must still be in the
  # live cmdline; otherwise this pid is not the daemon we recorded.
  local cmd
  cmd=$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null) || cmd=""
  if [[ -z "$marker" || "$cmd" != *"$marker"* ]]; then
    echo "[test-daemon-lib] refusing to signal pid $pid: cmdline no longer matches the recorded marker" >&2
    rm -f "$f"
    return 1
  fi

  # (3) SIGTERM, then SIGKILL after the grace window, then reap if possible
  # (wait is a no-op outside the shell that spawned the pid).
  local timeout="${TD_STOP_TIMEOUT:-15}" waited=0
  kill -TERM "$pid" 2>/dev/null || true
  while kill -0 "$pid" 2>/dev/null && (( waited < timeout )); do
    sleep 1
    waited=$((waited + 1))
  done
  if kill -0 "$pid" 2>/dev/null; then
    echo "[test-daemon-lib] pid $pid did not exit within ${timeout}s of SIGTERM — forcing it down" >&2
    kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
  rm -f "$f"
  return 0
}

# td_stop_all — stop every recorded pid, newest record first. Idempotent: the
# state files are removed as each pid is ended, so a second call (the EXIT
# trap after a signal trap already ran) is a no-op.
td_stop_all() {
  local files=() d
  for d in "$TD_STATE_DIR"/*; do
    [[ -f "$d" ]] || continue
    d=${d##*/}
    [[ "$d" =~ ^[0-9]+$ ]] || continue
    files+=("$d")
  done
  (( ${#files[@]} > 0 )) || return 0

  local reversed=() i
  for ((i = ${#files[@]} - 1; i >= 0; i--)); do
    reversed+=("${files[$i]}")
  done

  local f rc=0
  for f in "${reversed[@]}"; do
    td_stop_one "$f" || rc=1
  done
  return "$rc"
}

# --------------------------------------------------------------------- traps:

# _td_on_exit — the EXIT handler: preserve the script's original exit status
# (including a non-zero failure status), then td_stop_all, then the optional
# TD_EXTRA_CLEANUP function.
_td_on_exit() {
  local rc=$?
  td_stop_all
  if [[ -n "${TD_EXTRA_CLEANUP:-}" ]] && declare -F "$TD_EXTRA_CLEANUP" >/dev/null 2>&1; then
    "$TD_EXTRA_CLEANUP" || true
  fi
  # Explicit exit so the preserved status is deterministic even mid-trap.
  exit "$rc"
}

# td_install_traps — wire every exit path to td_stop_all:
#   EXIT  -> td_stop_all (and TD_EXTRA_CLEANUP), original status preserved
#   INT   -> td_stop_all, exit 130
#   TERM  -> td_stop_all, exit 143
#   HUP   -> td_stop_all, exit 129
# The signal traps run td_stop_all inline: the parent shell (still alive in
# the trap) signals the daemons, waits them out and reaps them, then exits
# with the signal's conventional status. The EXIT trap then sees the state
# files already gone and finishes as a no-op.
# Install this before the first td_start so an early failure still cleans up.
td_install_traps() {
  trap '_td_on_exit' EXIT
  trap 'td_stop_all >/dev/null 2>&1; trap - INT; kill -INT $$ 2>/dev/null; exit 130' INT
  trap 'td_stop_all >/dev/null 2>&1; trap - TERM; kill -TERM $$ 2>/dev/null; exit 143' TERM
  trap 'td_stop_all >/dev/null 2>&1; trap - HUP; kill -HUP $$ 2>/dev/null; exit 129' HUP
}

# ----------------------------------------------------------------- port wait:

# _td_port_free PORT — 0 when nothing listens on 127.0.0.1:PORT. Prefers ss;
# falls back to a bash /dev/tcp connect probe (which also catches wildcard
# listeners that answer on loopback).
_td_port_free() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    local addrs
    addrs=$(ss -Hltn 2>/dev/null | awk '{print $4}') || addrs=""
    if grep -Fxq "127.0.0.1:$port" <<<"$addrs"; then
      return 1
    fi
    # 0.0.0.0 or a :: wildcard still answers connections on the loopback.
    if grep -Eq "^(0\.0\.0\.0|::|\[::\]):$port" <<<"$addrs"; then
      return 1
    fi
    return 0
  fi
  (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && return 1
  return 0
}

# td_wait_port_closed PORT [TIMEOUT] — poll until nothing listens on
# 127.0.0.1:PORT (returns 0), else return 1 after TIMEOUT seconds (default 15).
td_wait_port_closed() {
  local port="$1" timeout="${2:-15}" waited=0
  [[ "$port" =~ ^[0-9]+$ ]] || return 1
  (( timeout >= 0 )) || return 1
  while :; do
    _td_port_free "$port" && return 0
    (( waited >= timeout )) && return 1
    sleep 1
    waited=$((waited + 1))
  done
}
