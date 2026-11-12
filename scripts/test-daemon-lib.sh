#!/usr/bin/env bash
# test-daemon-lib.sh — shared lifecycle helpers for every script and story
# that starts a datawatch daemon. Source it — it only defines functions and
# one default.
#
# Why: daemons (and the sessions they start) must be ended on EVERY exit
# path of the process that started them — normal exit, failure, and
# interrupt. Leaving them behind holds ports and keeps claude sessions open
# for days.
#
#   td_start <binary> <config> <logfile> [extra...]  start a daemon in the
#                                                     background, as
#                                                     `<binary> start
#                                                     --foreground --config
#                                                     <config> [extra...]`,
#                                                     record it, set
#                                                     TD_LAST_PID
#   td_track_pid <pid> <marker>      record a daemon started elsewhere
#   td_track_container <name-or-id>  record a Docker container started here;
#                                    resolved to the canonical 12-char id
#                                    (a 12-char or 64-char id is accepted
#                                    directly)
#   td_set_api <pid> <base_url> <token>   record API to end sessions first
#   td_stop_one <pid>                end one recorded daemon
#   td_stop_all                      end every recorded daemon (all owner
#                                    scopes), newest first
#   td_stop_self                     end this owner's recorded daemons only
#   td_alive <pid>                   0 iff the pid still exists (use in
#                                    conditionals — safe under `set -e`)
#   td_is_recorded <pid>             0 iff a record for this pid exists in
#                                    this shell's scope
#   td_install_traps ["$@"]          EXIT/INT/TERM/HUP traps that, on every
#                                    exit path, run TD_PRE_STOP_CLEANUP
#                                    (if set), then td_stop_all, then
#                                    TD_EXTRA_CLEANUP (if set), preserving
#                                    exit status
#                                    (idempotent: calling it twice does not
#                                    clobber what a first call registered)
#
# Optional cleanup hooks — both names of shell functions, both run on
# EVERY exit path (normal, failure, INT, TERM, HUP), both at most once:
#   TD_PRE_STOP_CLEANUP  runs BEFORE td_stop_all, while the recorded
#                        daemons are still alive: cleanup that talks to a
#                        daemon's own API (tracked-resource deletion,
#                        ending sessions by id) belongs here.
#   TD_EXTRA_CLEANUP     runs AFTER td_stop_all: filesystem cleanup (scratch
#                        dirs) that must never happen while its daemon is
#                        still running belongs here.
#   Hooks run in their own subshell: a hook that `exit`s or fails cannot cut
#   the shared cleanup off mid-run (the state wipe included) and cannot
#   change the script's final status. That status — the script's own
#   pending one, or 128+signal after INT/TERM/HUP — is captured before the
#   first hook runs and available to hooks as TD_EXIT_STATUS.
#   td_wait_port_closed <port> [timeout]   wait until nothing listens
#
# Ownership scope: TD_STATE_DIR is shared by a runner shell and the story
# shells inside it (exported), so records are namespaced by an optional
# owner — TD_OWNER. A parallel worker subshell exports TD_OWNER=<story>,
# so its daemons live in $TD_STATE_DIR/<owner>/ and its EXIT trap ends them
# at story end (td_stop_self) without touching the runner's daemons or
# sibling workers'. Without an owner (runner shell, and stories running
# serially inside it) records live in $TD_STATE_DIR itself.
#
#   $TD_STATE_DIR/[<owner>/]<pid>.rec:  pid=, marker=, api_base=,
#                                       api_token=, seq= [, kind=container]
#
# Rule: only PIDs this library recorded are ever signalled, and only while
# the marker recorded with them is still present in /proc/<pid>/cmdline
# (recycled pids are left alone). Container records are operated on by
# their recorded 12-char id first (the name recorded at track time is the
# fallback). No name searches; the tmux server is never touched.
#
# td_install_traps must be called early in a script, before any other side
# effect, and be given the script's own arguments ("$@"): when the script is
# started as a background job SIGINT is inherited as SIG_IGN and bash cannot
# trap it, so td_install_traps reruns the top-level script once, with those
# arguments, through a launcher that resets the signal to its default
# (TD_REEXEC_GUARD, holding the pid that was rerun, prevents a loop).

TD_STATE_DIR="${TD_STATE_DIR:-$(mktemp -d /tmp/dw-daemon-state.XXXXXX)}"
TD_OWNER="${TD_OWNER:-}"
TD_EXIT_STATUS="" # set by the handlers: the status the script is ending with
export TD_STATE_DIR
TD_STOP_TIMEOUT="${TD_STOP_TIMEOUT:-15}"
TD_API_TIMEOUT="${TD_API_TIMEOUT:-10}"

mkdir -p "$TD_STATE_DIR" 2>/dev/null || true

_td_now() { if command -v date >/dev/null 2>&1; then date +%s 2>/dev/null || printf '0\n'; else printf '0\n'; fi; }
_td_scope() {
  if [[ -n "$TD_OWNER" ]]; then
    printf '%s/%s' "$TD_STATE_DIR" "$TD_OWNER"
  else
    printf '%s' "$TD_STATE_DIR"
  fi
}
_td_rec() { printf '%s/%s.rec' "$(_td_scope)" "$1"; }

_td_get() {
  # _td_get <pid> <key> — print value of key from the record (empty if absent)
  local rec="$(_td_rec "$1")"
  [[ -f "$rec" ]] || return 0
  sed -n "s/^$2=//p" "$rec" 2>/dev/null | sed -n 1p
}

_td_put() {
  # _td_put <pid> <marker> [api_base] [api_token]
  local pid="$1" marker="$2" api_base="${3:-}" api_token="${4:-}"
  local dir f tmp
  dir="$(_td_scope)"
  f="$dir/$pid.rec"
  mkdir -p "$dir" 2>/dev/null || true
  tmp="$dir/.tmp.$$"
  {
    echo "pid=$pid"
    echo "marker=$marker"
    echo "api_base=$api_base"
    echo "api_token=$api_token"
    echo "seq=$(_td_get "$pid" "seq")"
  } > "$tmp" 2>/dev/null || { rm -f "$tmp" 2>/dev/null; return 1; }
  mv "$tmp" "$f" 2>/dev/null || { rm -f "$tmp" 2>/dev/null; return 1; }
}

_td_alive() { [[ -n "${1:-}" ]] && kill -0 "$1" 2>/dev/null; }

# td_alive <pid> — public form, safe in `set -e` conditionals: 0 iff the
# process still exists, 1 otherwise (including if it already exited).
td_alive() { _td_alive "$1"; }

# td_is_recorded <pid> — 0 iff a record exists in this shell's scope.
td_is_recorded() { [[ -n "${1:-}" && -f "$(_td_rec "$1")" ]]; }

_td_marker_ok() {
  # 0 iff /proc/$1/cmdline still contains $2 (empty marker never matches —
  # an empty marker means "I do not know what this was", so nothing may be
  # signalled on its strength)
  local cmd
  [[ -n "${2:-}" && -n "${1:-}" ]] || return 1
  cmd=$(tr '\0' ' ' < "/proc/$1/cmdline" 2>/dev/null || true)
  [[ -n "$cmd" && "$cmd" == *"$2"* ]]
}

td_track_pid() {
  # td_track_pid <pid> <marker>
  local pid="${1:?td_track_pid <pid> <marker>}"
  local marker="${2:?td_track_pid <pid> <marker>}"
  local rec old_seq="" seq s
  rec="$(_td_rec "$pid")"
  if [[ -f "$rec" ]]; then
    _td_put "$pid" "$marker" "$(_td_get "$pid" api_base)" "$(_td_get "$pid" api_token)"
  else
    if [[ -d "$TD_STATE_DIR" ]]; then
      while IFS= read -r s; do
        if [[ -n "$s" && ( -z "$old_seq" || "$s" -gt "$old_seq" ) ]]; then
          old_seq="$s"
        fi
      done < <(
        for r in "$TD_STATE_DIR"/*.rec "$TD_STATE_DIR"/*/*.rec; do
          if [[ -f "$r" ]]; then sed -n 's/^seq=//p' "$r" 2>/dev/null; fi
        done)
    fi
    [[ -n "$old_seq" ]] || old_seq=0
    seq=$((old_seq + 1))
    _td_put "$pid" "$marker" "" "" || return 1
    echo "seq=$seq" >> "$rec" # seq after the base fields; newest records have larger seq
  fi
}

td_track_container() {
  # td_track_container <name-or-id>
  # Record a Docker container so a trap can end it (docker stop by id,
  # recorded name as fallback, then rm). The container must exist at record
  # time. Resolution: `docker inspect` of the argument yields the FULL 64
  # hex id (that is what {{.Id}} prints), so both the 12-char short and the
  # 64-char long form are accepted at any entry point and normalized here
  # to the 12-char form used as the record key — a caller that passes or
  # stores either shape ends up with the same record.
  local cid="${1:?td_track_container <name-or-id>}" idfull id="none" name="${1:-}"
  if idfull=$(docker inspect --format '{{.Id}}' "$cid" 2>/dev/null) && [[ -n "$idfull" ]]; then
    idfull=${idfull//[[:space:]]/}
    if [[ "$idfull" =~ ^[a-f0-9]{64}$ ]]; then
      id="${idfull:0:12}"
    elif [[ "$idfull" =~ ^[a-f0-9]{12}$ ]]; then
      id="$idfull"
    fi
  elif [[ "$cid" =~ ^[a-f0-9]{12}$ || "$cid" =~ ^[a-f0-9]{64}$ ]]; then
    # Docker is unavailable or refuses to resolve, but the caller handed us
    # a well-formed id: trust that shape.
    id="${cid:0:12}"
    name="${cid:0:12}"
  fi
  if [[ "$id" == "none" ]]; then
    echo "test-daemon: td_track_container: cannot resolve '$cid' to a 12-char container id (docker inspect failed or non-hex id)" >&2
    return 1
  fi
  if [[ -z "$name" || ! "$name" =~ ^[a-f0-9]+$ ]]; then
    name="$id"
  fi
  local rec="$(_td_rec "$id")"
  _td_put "$id" "$name" "" "" || return 1
  echo "kind=container" >> "$rec"
}

td_set_api() {
  # td_set_api <pid> <base_url> [token] — remember the API so
  # stopping ends its sessions first (best-effort)
  local pid="${1:?td_set_api <pid> <base_url> <token>}"
  local base="${2:?td_set_api <pid> <base_url>}" token="${3:-}"
  local rec="$(_td_rec "$pid")"
  [[ -f "$rec" ]] || { echo "test-daemon: td_set_api: no record for pid $pid" >&2; return 1; }
  # Rewritten line by line: a URL or token may hold any character, so no
  # sed expression (its delimiter) is built from them.
  local tmp="$rec.tmp.$$" line
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      api_base=*) printf 'api_base=%s\n' "$base" ;;
      api_token=*) printf 'api_token=%s\n' "$token" ;;
      *) printf '%s\n' "$line" ;;
    esac
  done < "$rec" > "$tmp" || { rm -f "$tmp"; return 1; }
  mv "$tmp" "$rec"
}

td_probe_pid() {
  # td_probe_pid <pid> — sets TD_PID_STATE (the process state letter) and
  # TD_PID_STATE_RC: 0 = found, 1 = gone, 2 = not determinable (no /proc
  # and no ps(1); the pid may or may not exist). Reads state without
  # signalling; always returns 0 so it is safe under `set -e` anywhere.
  TD_PID_STATE=""
  TD_PID_STATE_RC=2
  local pid="${1:-}" rest=""
  if [[ -z "$pid" || ! "$pid" =~ ^[0-9]+$ ]]; then
    return 0
  fi
  if [[ -r "/proc/$pid/stat" ]]; then
    if IFS= read -r -d '' rest < "/proc/$pid/stat" 2>/dev/null; then
      : # the whole stat line is loaded even though read exits nonzero at EOF
    fi
    if [[ -n "${rest:-}" ]]; then
      rest="${rest##*) }"
      TD_PID_STATE="${rest%% *}"
      TD_PID_STATE_RC=0
    else
      TD_PID_STATE_RC=1
    fi
    return 0
  fi
  rest="$(ps -o state= -p "$pid" 2>/dev/null | tr -d '[:space:]')"
  if [[ -n "$rest" ]]; then
    TD_PID_STATE="$rest"
    TD_PID_STATE_RC=0
    return 0
  fi
  if kill -0 "$pid" 2>/dev/null; then
    TD_PID_STATE_RC=2
  else
    TD_PID_STATE_RC=1
  fi
  return 0
}

td_start() {
  # td_start <binary> <config> <logfile> [extra args...]
  # Start the daemon in the background as
  #   <binary> start --foreground --config <config> [extra args...]
  # record it, set TD_LAST_PID. The config path is the marker: it is in the
  # daemon's cmdline for as long as the daemon runs. A start that left
  # nothing alive (missing binary, failed log redirection, immediate exit)
  # is reported: td_start probes the child's process state in a short window
  # and returns the child's exit code (or 1 if it reaped itself cleanly),
  # so the caller never mistakes a dead daemon for a running one. Under
  # `set -e` this call must run in a failure-conditional context (it
  # legitimately fails when the record cannot be written or the daemon dies
  # at start).
  local bin="${1:?td_start <binary> <config> <logfile> [extra args...]}" cfg="${2:?}" log="${3:?}"
  shift 3
  if command -v setsid >/dev/null 2>&1; then
    setsid "$bin" start --foreground --config "$cfg" "$@" </dev/null >>"$log" 2>&1 &
  else
    "$bin" start --foreground --config "$cfg" "$@" </dev/null >>"$log" 2>&1 &
  fi
  local pid=$!
  TD_LAST_PID="$pid"
  td_track_pid "$TD_LAST_PID" "$cfg"
  local i state rcstate dead=0
  for i in 1 2 3 4 5 6 7 8 9 10; do
    td_probe_pid "$pid"
    state="$TD_PID_STATE"; rcstate="$TD_PID_STATE_RC"
    case "$state" in
      R*|S*|D*) return 0 ;;
    esac
    if [[ -z "$state" && "$rcstate" -eq 2 ]]; then
      return 0
    fi
    if [[ "$rcstate" -eq 1 || "$state" == Z* ]]; then
      dead=1
      break
    fi
    sleep 0.05
  done
  if [[ "$dead" -eq 1 ]]; then
    local code=0 realpid="" d
    wait "$pid" 2>/dev/null && code=0 || code=$?
    # Rare edge: the wrapper that spawned the daemon (a setsid that had to
    # fork) can exit while the daemon itself is alive under another pid. A
    # start that left nothing live must fail, so before failing look for the
    # daemon under our own config marker; if it is alive, re-record it and
    # succeed, so cleanup still owns exactly it. cmdline args are NUL
    # separated: match the --config token first, then the exact "--config
    # <cfg>" pair after translating NULs to spaces.
    local d
    for d in /proc/[0-9]*; do
      grep -qFa -- '--config' "$d/cmdline" 2>/dev/null || continue
      if tr '\0' ' ' < "$d/cmdline" 2>/dev/null | grep -qF -- "--config $cfg" 2>/dev/null; then
        realpid="${d#/proc/}"
        break
      fi
    done
    if [[ -n "$realpid" ]]; then
      TD_LAST_PID="$realpid"
      td_track_pid "$realpid" "$cfg"
      return 0
    fi
    if [[ "$code" -gt 0 ]]; then
      return "$code"
    fi
    return 1
  fi
  return 0
}

_td_end_sessions() {
  # _td_end_sessions <pid> — best-effort: end the daemon's live claude
  # sessions through its own API before signalling, so state gets written
  # even if the daemon kill is abrupt. The daemon's kill endpoint takes one
  # session at a time (POST /api/sessions/kill {"id": full_id}), so list the
  # non-terminal ones (running / waiting_input / rate_limited) and kill
  # each of them individually.
  local base="$(_td_get "$1" "api_base")" token="$(_td_get "$1" "api_token")"
  local curl
  curl=$(readlink -f "$(command -v curl 2>/dev/null)" 2>/dev/null || command -v curl 2>/dev/null || true)
  [[ -n "$base" && -n "${curl:-}" ]] || return 0
  local resp sids sid
  if resp=$(curl -skS --max-time "$TD_API_TIMEOUT" \
      ${token:+-H "Authorization: Bearer $token"} \
      "$base/api/sessions" 2>/dev/null); then
    if sids=$(echo "$resp" | python3 -c '
import json,sys
try:
    d = json.load(sys.stdin)
except Exception:
    raise SystemExit(0)
for item in d if isinstance(d, list) else d.get("sessions", []):
    if item.get("state") in ("running", "waiting_input", "rate_limited"):
        print(item.get("full_id") or item.get("session_id") or item.get("id") or item.get("name") or "")
' 2>/dev/null); then
      for sid in $sids; do
        [[ -n "$sid" ]] || continue
        curl -skS --max-time "$TD_API_TIMEOUT" \
          ${token:+-H "Authorization: Bearer $token"} \
          -X POST -H "Content-Type: application/json" \
          -d '{"id":"'$sid'"}' \
          "$base/api/sessions/kill" >/dev/null 2>&1 \
          || echo "warn: could not end session $sid via $base" >&2
      done
    fi
  else
    echo "warn: could not list sessions from $base before stopping its daemon" >&2
  fi
  return 0
}

_td_wait_dead() {
  # _td_wait_dead <pid> <timeout> — 0 iff the pid no longer exists
  local end
  end=$((SECONDS + $2))
  while _td_alive "$1"; do
    if ((SECONDS >= end)); then
      return 1
    fi
    sleep 0.2
  done
  return 0
}

td_stop_one() {
  # td_stop_one <pid> — end one recorded daemon
  # Returns 0 when the daemon is gone (or was already gone / recycled and
  # left alone), 1 when it is still running. If it survives SIGKILL its
  # record is KEPT (for a later pass) and the failure is reported — a lie
  # here would let a caller delete scratch while the daemon still runs.
  local pid="${1:?td_stop_one <pid>}"
  local rec="$(_td_rec "$pid")"
  [[ -f "$rec" ]] || return 0
  local kind marker api_base
  kind="$(sed -n 's/^kind=//p' "$rec" | sed -n 1p)"
  marker="$(_td_get "$pid" "marker")"
  api_base="$(_td_get "$pid" "api_base")"

  if [[ "$kind" == "container" ]]; then
    # $pid is the recorded canonical 12-char id; $marker is the name it was
    # recorded under (the fallback). Operate by id first — never by name
    # search.
    local cmsg
    if cmsg=$(docker stop "$pid" 2>&1) && cmsg="stopped recorded container ${pid} (${marker})"; then :
    elif cmsg=$(docker stop "$marker" 2>&1) && cmsg="stopped recorded container by name ${marker}"; then :
    elif cmsg=$(docker rm "$pid" 2>&1) && cmsg="removed stopped container ${pid} (${marker})"; then :
    elif cmsg=$(docker rm "$marker" 2>&1) && cmsg="removed stopped container ${marker}"; then :
    elif ! docker inspect "$pid" >/dev/null 2>&1 && ! docker inspect "$marker" >/dev/null 2>&1; then
      cmsg="recorded container ${pid} (${marker}) is already gone"
    else
      echo "test-daemon: FAILED: could not end recorded container ${pid} (${marker})" >&2
      return 1
    fi
    echo "  daemon: $cmsg"
    rm -f "$(_td_rec "$pid")"
    return 0
  fi

  if [[ -n "$api_base" ]]; then _td_end_sessions "$pid"; fi
  if _td_alive "$pid"; then
    if ! _td_marker_ok "$pid" "$marker"; then
      echo "  daemon: pid $pid no longer carries its recorded marker ($marker); not signalling it" >&2
    else
      kill -TERM "$pid" 2>/dev/null || true
      if ! _td_wait_dead "$pid" "$TD_STOP_TIMEOUT" && _td_alive "$pid"; then
        if _td_marker_ok "$pid" "$marker"; then
          kill -KILL "$pid" 2>/dev/null || true
          if _td_wait_dead "$pid" 5; then
            :
          elif _td_alive "$pid"; then
            echo "test-daemon: pid $pid survives SIGKILL; keeping its record for a later pass" >&2
            return 1
          fi
        else
          echo "  daemon: pid $pid lost its recorded marker ($marker) before SIGKILL; not signalling it" >&2
        fi
      fi
    fi
  fi
  _td_alive "$pid" || wait "$pid" 2>/dev/null || true
  rm -f "$(_td_rec "$pid")"
  return 0
}

_collect_stops() {
  # _collect_stops <dir> — print "<seq> <pid>" (sorted oldest first) from one record dir
  local f pid seq
  for f in "$1"/*.rec; do
    [[ -f "$f" ]] || continue
    pid="${f##*/}"
    pid="${pid%.rec}"
    seq="$(sed -n 's/^seq=//p' "$f" | sed -n 1p)"
    printf '%s %s\n' "${seq:-0}" "$pid"
  done | sort
  return 0
}

td_stop_all() {
  # End every recorded daemon in every scope, oldest first (so a fresh
  # story-daemon is ended after a runner daemon it replaced)
  local entry pid root
  entry="$(
    for root in "$TD_STATE_DIR" "$TD_STATE_DIR"/*; do
      if [[ -d "$root" ]]; then
        _collect_stops "$root"
      fi
    done | LC_ALL=C sort -k1,1n
  )"
  while read -r _ pid; do
    if [[ -n "$pid" ]]; then
      td_stop_one "$pid" || true
    fi
  done <<< "$entry"
  return 0
}

td_stop_self() {
  # End only this owner's recorded daemons (the worker subshell's own
  # EXIT trap uses this; td_stop_all stays available to the runner)
  local dir="$(_td_scope)" entry
  [[ -d "$dir" ]] && {
    entry="$(_collect_stops "$dir")"
    while read -r _ pid; do
      if [[ -n "$pid" ]]; then
        td_stop_one "$pid" || true
      fi
    done <<< "$entry"
  }
  return 0
}

td_wait_port_closed() {
  # 0 iff the port stops accepting connections within timeout seconds
  local port="${1:?td_wait_port_closed <port> [timeout]}" timeout="${2:-15}"
  local end
  end=$((SECONDS + timeout))
  while timeout 1 bash -c "exec 3<>/dev/tcp/127.0.0.1/$port" 2>/dev/null; do
    if ((SECONDS >= end)); then
      return 1
    fi
    sleep 0.5
  done
  return 0
}

_td_sigint_ignored() {
  # 0 iff SIGINT arrived as SIG_IGN (bash cannot install a SIGINT trap then):
  # bit 2 of the SigIgn mask in this process's /proc status
  local mask
  mask=$(sed -n 's/^SigIgn:[[:space:]]*//p' "/proc/$$/status" 2>/dev/null | sed -n 1p)
  [[ "$mask" =~ ^[0-9a-fA-F]+$ ]] && (( (0x$mask & 2) != 0 ))
}

_td_reexec_with_int() {
  # _td_reexec_with_int [script args...] — if SIGINT was inherited as SIG_IGN
  # (this script started as a background job), rerun the top-level script
  # once with its arguments through a launcher that resets the signal to its
  # default. The guard holds the pid that was rerun (an exec keeps the pid),
  # so the rerun does not loop while a child script of its own still can.
  # Best effort: without python3, or without a script file to rerun, the
  # traps are installed anyway (EXIT and TERM still work).
  local src="${BASH_SOURCE[${#BASH_SOURCE[@]}-1]:-${0:-}}"
  if _td_sigint_ignored && [[ "${TD_REEXEC_GUARD:-}" != "$$" && -f "$src" ]] \
      && command -v python3 >/dev/null 2>&1; then
    export TD_REEXEC_GUARD=$$
    exec python3 -I -c '
import os, signal, sys
signal.signal(signal.SIGINT, signal.SIG_DFL)
os.execvp("bash", ["bash"] + sys.argv[1:])
' "$src" "$@"
  fi
  return 0
}

# _td_run_hook — run a registered TD_*_CLEANUP hook (a shell function name)
# in its own subshell: an `exit` or failure inside the hook cannot cut the
# shared handler off mid-cleanup, and the hook cannot change the script's
# recorded exit status.
_td_run_hook() {
  local hook="${1:-}"
  [[ -n "$hook" ]] || return 0
  if declare -f "$hook" >/dev/null 2>&1; then
    ( "$hook" || true )
  fi
  return 0
}

_td_run_pre_stop_hook() {
  # Runs the cleanup that must talk to a recorded daemon's own API
  # (tracked-resource deletion, ending sessions by id) while the daemon
  # is still alive. Consumed by whichever handler is reached first, so
  # exactly one exit path runs it.
  local hook="${TD_PRE_STOP_CLEANUP:-}"
  TD_PRE_STOP_CLEANUP=""
  _td_run_hook "$hook"
}

_td_on_exit() {
  TD_EXIT_STATUS="$?" # first line: $? is still the script's own pending status
  _td_run_pre_stop_hook
  td_stop_all
  # Story scripts may register their own cleanup (e.g. scratch dirs) here;
  # td_stop_all runs first so their functions never leak daemons. The hook
  # runs in a subshell, so it cannot abort the state wipe below or change
  # this handler's (and the script's) exit status.
  _td_run_hook "${TD_EXTRA_CLEANUP:-}"
  if [[ -z "${TD_OWNER:-}" ]]; then
    # Only the owner-less shell owns the shared state dir (a worker
    # subshell with TD_OWNER set must not delete the dir its runner is
    # still writing records into).
    find "$TD_STATE_DIR" -maxdepth 1 -type f -delete 2>/dev/null || true
    rmdir "$TD_STATE_DIR" 2>/dev/null || true
  fi
}

_td_on_signal() {
  # Run the pre-stop hook while the daemons are still alive, end every
  # recorded daemon, then exit with the conventional 128+signal status
  # (the EXIT trap still runs; its pre-stop hook slot was consumed here).
  # The signal is deliberately not sent to ourselves again: its trap is
  # still installed, so the handler would re-enter itself.
  local sig="$1" code=1
  case "$sig" in
    HUP) code=129 ;;
    INT) code=130 ;;
    TERM) code=143 ;;
  esac
  # The signal means the script ends with 128+signal; record it before any
  # hook runs so a hook `exit` (subshell only) cannot change it.
  TD_EXIT_STATUS="${TD_EXIT_STATUS:-$code}"
  _td_run_pre_stop_hook
  td_stop_all
  exit "$code"
}

td_install_traps() {
  # td_install_traps ["$@"] — pass the calling script's arguments so a rerun
  # (see _td_reexec_with_int) keeps them.
  _td_reexec_with_int "$@"
  if [[ -n "${TD_TRAPS_INSTALLED:-}" ]]; then
    return 0
  fi
  TD_TRAPS_INSTALLED=1
  trap _td_on_exit EXIT
  trap '_td_on_signal INT' INT
  trap '_td_on_signal TERM' TERM
  trap '_td_on_signal HUP' HUP
}
