#!/usr/bin/env bash
# test-daemon-lib.sh — shared lifecycle helper for every script or story that
# starts a datawatch daemon. Source it; it only defines functions and defaults.
#
# Why it exists: a daemon (and the sessions it started) must end on every exit
# path of the job that started it: normal exit, failure and interrupt. Leaving
# one behind kept ports and a claude session alive for days.
#
#   td_start <binary> <config> <logfile>   start a daemon in the background,
#                                          record it, set TD_LAST_PID
#   td_track_pid <pid> <marker>            record a daemon started elsewhere
#   td_track_container <id>                record a Docker container started here
#   td_set_api <pid> <base_url> <token>    record its API so sessions end first
#   td_stop_one <pid>                      end one recorded daemon
#   td_stop_all                            end every recorded daemon, newest first
#   td_install_traps                       EXIT/INT/TERM/HUP traps that call
#                                          td_stop_all and keep the exit status
#   td_wait_port_closed <port> [timeout]   wait until nothing listens on it
#
# Rules: only a PID this library recorded is ever signalled, and only while
# /proc/<pid>/cmdline still carries the marker recorded with it (a recycled PID
# is left alone). No name search of any kind, and no tmux server is touched.
#
# State lives in files under TD_STATE_DIR (one file per recorded PID) so a story
# running in a subshell and the runner that owns the traps see the same records.
#
# Call td_install_traps near the top of the script, before any side effect: when
# the shell was started as a background job SIGINT is inherited as ignored and
# bash cannot trap it, so td_install_traps re-runs the script once through a
# launcher that resets that signal.

TD_STATE_DIR="${TD_STATE_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/test-daemon.XXXXXX")}"
export TD_STATE_DIR
TD_STOP_TIMEOUT="${TD_STOP_TIMEOUT:-15}"
TD_LAST_PID=""
TD_LAST_CONTAINER=""

# _td_rec <pid> — path of the record file for a PID.
_td_rec() { printf '%s/%s.rec' "$TD_STATE_DIR" "$1"; }

# _td_get <pid> <key> — one field from a record (empty when absent).
_td_get() {
  local f line
  f="$(_td_rec "$1")"
  [[ -f "$f" ]] || return 0
  while IFS= read -r line; do
    if [[ "$line" == "$2="* ]]; then
      printf '%s' "${line#*=}"
      return 0
    fi
  done < "$f"
}

# _td_put <pid> <marker> <api_base> <token> [seq] — write a whole record. The
# seq (nanosecond clock) orders td_stop_all; it is kept when a record is updated.
_td_put() {
  local f seq="${5:-}"
  mkdir -p "$TD_STATE_DIR"
  [[ -n "$seq" ]] || seq="$(date +%s%N)"
  f="$(_td_rec "$1")"
  printf 'pid=%s\nmarker=%s\napi_base=%s\napi_token=%s\nseq=%s\n' \
    "$1" "$2" "$3" "$4" "$seq" > "$f.tmp.$$" && mv -f "$f.tmp.$$" "$f"
}

# _td_alive <pid> — running, and not a zombie waiting to be reaped.
_td_alive() {
  local stat
  kill -0 "$1" 2>/dev/null || return 1
  [[ -r "/proc/$1/stat" ]] || return 1
  stat=$(< "/proc/$1/stat") 2>/dev/null || return 1
  stat="${stat##*) }"
  [[ "${stat%% *}" != Z ]]
}

# _td_marker_ok <pid> <marker> — the live cmdline still carries the marker.
_td_marker_ok() {
  local cmd
  [[ -n "$2" ]] || return 1
  [[ -r "/proc/$1/cmdline" ]] || return 1
  cmd="$(tr '\0' ' ' < "/proc/$1/cmdline")" || return 1
  [[ "$cmd" == *"$2"* ]]
}

td_track_pid() {
  local pid="${1:-}" marker="${2:-}"
  [[ -n "$pid" && -n "$marker" ]] || return 1
  _td_put "$pid" "$marker" "" ""
}

# td_track_container <id> — record a Docker container this script started so
# the shared EXIT/INT/TERM/HUP traps end it (docker stop, then docker rm, by
# the recorded id only) on every exit path, the same as a recorded daemon.
# <id> may be the container name used with docker run; when docker can
# resolve it, the canonical 12-character id becomes the record key instead.
td_track_container() {
  local cid="${1:-}" id12
  [[ -n "$cid" ]] || return 1
  id12="$(docker inspect --format '{{.Id}}' "$cid" 2>/dev/null)"
  [[ "$id12" =~ ^[a-f0-9]{12}$ ]] || id12=""
  [[ -n "$id12" ]] || { echo "test-daemon: cannot resolve container id for '$cid'" >&2; return 1; }
  _td_put "$id12" "$cid" "" ""
  printf 'kind=container\n' >> "$( _td_rec "$id12")"
  TD_LAST_CONTAINER="$id12"
  return 0
}

td_set_api() {
  local pid="${1:-}" base="${2:-}" token="${3:-}"
  [[ -n "$pid" ]] || return 1
  _td_put "$pid" "$(_td_get "$pid" marker)" "$base" "$token" "$(_td_get "$pid" seq)"
}

td_start() {
  local bin="${1:?td_start <binary> <config> <logfile> [extra args...]}" cfg="${2:?}" log="${3:?}"
  shift 3
  # Optional extra args (e.g. a final --port override) follow the library's
  # own start arguments. The recorded marker stays the config path, which is
  # still in the daemon's cmdline.
  local -a extra=("$@")
  # env -u keeps the daemon off any tmux server the caller is inside.
  env -u TMUX -u TMUX_TMPDIR "$bin" start --foreground --config "$cfg" ${extra[@]+"${extra[@]}"} >> "$log" 2>&1 &
  TD_LAST_PID=$!
  # The forked shell has the caller's cmdline until exec; record once it is ours.
  for _ in {1..20}; do
    _td_marker_ok "$TD_LAST_PID" "$cfg" && break
    sleep 0.1
  done
  td_track_pid "$TD_LAST_PID" "$cfg"
}

# _td_end_sessions <pid> — end every session the daemon serves through its own
# API (the daemon starts claude sessions the signal alone would not end). Ids
# come from "full_id" in the bare JSON array GET /api/sessions returns. Short
# timeouts, every failure ignored: the daemon may already be gone.
_td_end_sessions() {
  local base token body id
  base="$(_td_get "$1" api_base)"
  token="$(_td_get "$1" api_token)"
  [[ -n "$base" ]] || return 0
  command -v curl >/dev/null 2>&1 || return 0
  body="$(curl -sk --connect-timeout 2 --max-time 5 \
    -H "Authorization: Bearer $token" "${base%/}/api/sessions" 2>/dev/null)" || return 0
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    curl -sk --connect-timeout 2 --max-time 5 -X POST \
      -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
      -d "{\"id\":\"$id\"}" "${base%/}/api/sessions/kill" >/dev/null 2>&1 || true
  done < <(printf '%s' "$body" | grep -o '"full_id" *: *"[^"]*"' | sed 's/.*: *"\(.*\)"/\1/')
  return 0
}

# _td_wait_dead <pid> <seconds> — poll until the PID is gone or the time is up.
_td_wait_dead() {
  local end=$((SECONDS + $2))
  while _td_alive "$1"; do
    ((SECONDS < end)) || return 1
    sleep 0.1
  done
}

td_stop_one() {
  local pid="${1:-}" marker kind
  [[ -n "$pid" && -f "$(_td_rec "$pid")" ]] || return 0
  kind="$(_td_get "$pid" kind)"
  marker="$(_td_get "$pid" marker)"
  if [[ "$kind" == "container" ]]; then
    # Container record (td_track_container): the marker field holds the
    # container name the stop was recorded with. docker stop, then docker rm,
    # by that id only — no name search, no other containers, no tmux.
    if docker stop "$marker" >/dev/null 2>&1; then
      echo "stopped recorded container $marker"
    elif docker rm "$marker" >/dev/null 2>&1; then
      echo "removed recorded container $marker (already exiting)"
    else
      echo "FAILED: could not stop recorded container $marker" >&2
      return 1
    fi
    rm -f "$(_td_rec "$pid")"
    return 0
  fi
  _td_end_sessions "$pid"
  if _td_alive "$pid"; then
    if ! _td_marker_ok "$pid" "$marker"; then
      echo "test-daemon: pid $pid no longer carries its marker; not signalling it" >&2
      rm -f "$(_td_rec "$pid")"
      return 0
    fi
    kill -TERM "$pid" 2>/dev/null || true
    if ! _td_wait_dead "$pid" "$TD_STOP_TIMEOUT"; then
      # Re-check right before the forced kill: the PID may have been recycled.
      if _td_marker_ok "$pid" "$marker"; then
        kill -KILL "$pid" 2>/dev/null || true
        _td_wait_dead "$pid" 5 || echo "test-daemon: pid $pid did not exit after SIGKILL" >&2
      fi
    fi
  fi
  # Reap it when it is our child (a no-op error otherwise); it is dead by now.
  _td_alive "$pid" || wait "$pid" 2>/dev/null || true
  rm -f "$(_td_rec "$pid")"
  return 0
}

td_stop_all() {
  local f seq pid
  [[ -d "$TD_STATE_DIR" ]] || return 0
  # Newest record first; the clock value in each record gives the order.
  while read -r _ pid; do
    [[ -n "$pid" ]] && td_stop_one "$pid"
  done < <(for f in "$TD_STATE_DIR"/*.rec; do
      [[ -f "$f" ]] || continue
      pid="${f##*/}"; pid="${pid%.rec}"
      seq="$(_td_get "$pid" seq)"
      printf '%s %s\n' "${seq:-0}" "$pid"
    done | sort -rn)
  return 0
}

td_wait_port_closed() {
  local port="${1:?td_wait_port_closed <port> [timeout]}" timeout="${2:-15}"
  local end=$((SECONDS + timeout))
  while ((SECONDS <= end)); do
    if command -v ss >/dev/null 2>&1; then
      [[ -z "$(ss -Hltn "sport = :$port" 2>/dev/null)" ]] && return 0
    else
      # A refused connect means nothing listens; the probe runs in a subshell.
      (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null || return 0
    fi
    sleep 0.2
  done
  return 1
}

# _td_sigint_ignored — SIGINT was ignored when this shell started (bit 1 of
# SigIgn), as for a background job of a non-interactive shell.
_td_sigint_ignored() {
  local mask
  mask="$(sed -n 's/^SigIgn:[[:space:]]*//p' /proc/$$/status 2>/dev/null)"
  [[ -n "$mask" ]] && (( (16#$mask & 2) != 0 ))
}

# _td_reexec_with_int — bash cannot trap a signal ignored on entry, so run the
# same command line again through a launcher that resets SIGINT to default.
_td_reexec_with_int() {
  local -a args=()
  if [[ -n "${TD_INT_REEXEC:-}" ]]; then
    unset TD_INT_REEXEC
    return 0
  fi
  _td_sigint_ignored || return 0
  command -v perl >/dev/null 2>&1 || return 0
  mapfile -d '' -t args < "/proc/$$/cmdline" 2>/dev/null || return 0
  ((${#args[@]} > 0)) || return 0
  export TD_INT_REEXEC=1
  shopt -s execfail
  exec perl -e '$SIG{INT} = "DEFAULT"; exec { $ARGV[0] } @ARGV' "${args[@]}"
  unset TD_INT_REEXEC
  return 0
}

# _td_on_exit — keep the script's own exit status, end every daemon, then run
# the optional caller cleanup named in TD_EXTRA_CLEANUP.
_td_on_exit() {
  local rc=$?
  trap - EXIT
  td_stop_all
  if [[ -n "${TD_EXTRA_CLEANUP:-}" ]] && declare -F "$TD_EXTRA_CLEANUP" >/dev/null; then
    "$TD_EXTRA_CLEANUP" || true
  fi
  rmdir "$TD_STATE_DIR" 2>/dev/null || true
  exit "$rc"
}

# _td_on_signal <status> — end every daemon, then exit 128+signal (the EXIT trap
# runs next and stops nothing twice).
_td_on_signal() {
  td_stop_all
  exit "$1"
}

td_install_traps() {
  _td_reexec_with_int
  trap _td_on_exit EXIT
  trap '_td_on_signal 130' INT
  trap '_td_on_signal 143' TERM
  trap '_td_on_signal 129' HUP
}
