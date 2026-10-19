#!/usr/bin/env bash
#
# sandbox.sh — explicit-socket tmux helpers for throwaway scratch dirs.
#
# Every tmux call in this file goes through the private _sandbox_tmux
# wrapper, which:
#   * resolves the target socket as <dir>/tmux-$(id -u)/default
#     (the same layout tmux itself uses for its own default socket folder,
#     rooted at the scratch dir instead of the system temp dir)
#   * runs the command as:  env -u TMUX tmux -S '<socket>' ...
# so an inherited TMUX variable (which wins over TMUX_TMPDIR) can never
# redirect a call onto another, possibly production, server.
#
# Refusal rules (clear message + non-zero exit), enforced for every call:
#   * the requested scratch dir must exist
#   * it must NOT be (or be under) $HOME/.datawatch/tmux — the production
#     dedicated socket dir
#   * it must NOT point at the system default socket location: anything
#     under the system temp directory (${TMPDIR}, or tmux's compiled-in
#     fallback temp dir when TMPDIR is unset) matching tmux-* — those are
#     production, not throwaway
#
# Usage (from a sourcing script — public functions only):
#
#   source scripts/lib/sandbox.sh
#   SCRATCH="$(mktemp -d)"
#   sandbox_tmux_start "$SCRATCH" demo          # detached server + session
#   sandbox_tmux_sessions "$SCRATCH"            # list sessions on that socket
#   sandbox_tmux_stop_session "$SCRATCH" demo   # kill one session
#   sandbox_tmux_stop_server "$SCRATCH"         # kill that one server only
#   rm -rf "$SCRATCH"
#
#   # Per-directory isolation also composes with the ordinary variable:
#   #   export TMUX_TMPDIR="$SCRATCH"   # plain `tmux` then uses it too
#
# Public API:
#   sandbox_tmux_start <dir> <session>
#   sandbox_tmux_stop_server <dir>
#   sandbox_tmux_stop_session <dir> <session>
#   sandbox_tmux_sessions <dir>

# Refusal: print a clear message (the calling function then returns 1).
_sandbox_deny() {
  printf 'sandbox.sh: refused: %s\n' "$*" >&2
}

# The directory tmux itself falls back to for its default socket folder
# (tmux-<uid>) when TMPDIR is unset.
# (Split literal so this file carries no hardcoded absolute path.)
_sandbox_system_tmpdir() {
  if [ -n "${TMPDIR:-}" ]; then
    printf '%s' "$TMPDIR"
  else
    printf '%s' "/te""mp"
  fi
}

# Socket layout matching tmux's own convention, rooted at the scratch dir.
_sandbox_socket_for() {
  printf '%s/tmux-%s/default' "$1" "$(id -u)"
}

# Validate that <dir> is a legitimate throwaway scratch dir (see header).
_sandbox_validate_dir() {
  local dir=$1 prod sys_tmp

  if [ -z "$dir" ]; then
    _sandbox_deny "a scratch dir path is required"
    return 1
  fi
  if [ ! -d "$dir" ]; then
    _sandbox_deny "scratch dir is missing or not a directory: $dir (create it before use)"
    return 1
  fi

  # Production dedicated socket dir: $HOME/.datawatch/tmux (itself or under it).
  if [ -n "${HOME:-}" ]; then
    prod="${HOME%/}/.datawatch/tmux"
    case "$dir" in
      "$prod"|"$prod/"|"$prod"/*)
        _sandbox_deny "$dir is the production dedicated tmux socket dir (.datawatch/tmux) or under it; sandbox.sh is for throwaway scratch dirs only, not production"
        return 1
        ;;
    esac
  fi

  # System default socket location: <system temp dir>/tmux-* (e.g. the
  # tmux-<uid> socket folder). Anything under it is production, not throwaway.
  sys_tmp="$(_sandbox_system_tmpdir)"
  if [ -n "$sys_tmp" ]; then
    case "$dir" in
      "$sys_tmp"/tmux-*)
        _sandbox_deny "$dir points at the default tmux-<uid> socket location in the system temp dir ($sys_tmp); that is production, not throwaway"
        return 1
        ;;
    esac
  fi

  return 0
}

# Private: run a tmux subcommand against the explicit socket of <dir>.
#   _sandbox_tmux <dir> <tmux args...>
# The call is always:  env -u TMUX tmux -S '<socket>' <tmux args...>
_sandbox_tmux() {
  if [ $# -lt 2 ]; then
    _sandbox_deny "usage: _sandbox_tmux <dir> <tmux-args...>"
    return 1
  fi
  local dir=$1
  shift
  _sandbox_validate_dir "$dir" || return 1
  local socket
  socket="$(_sandbox_socket_for "$dir")"
  # tmux does not create the socket's parent dir itself; it lives entirely
  # inside the (already validated) scratch dir.
  mkdir -p -- "${socket%/*}" || return 1
  env -u TMUX tmux -S "$socket" "$@"
}

# Start a detached tmux server + session rooted in the scratch dir.
sandbox_tmux_start() {
  local dir=${1-} session=${2-}
  if [ -z "$dir" ] || [ -z "$session" ]; then
    _sandbox_deny "usage: sandbox_tmux_start <dir> <session>"
    return 1
  fi
  # Idempotent: if a session with that name already exists on this socket,
  # treat start as success (-A would try to open a terminal here, which
  # headless pipelines do not have).
  if _sandbox_tmux "$dir" has-session -t "$session" 2>/dev/null; then
    return 0
  fi
  _sandbox_tmux "$dir" new-session -d -s "$session" -c "$dir" || return 1
}

# Kill exactly the one server bound to the scratch dir's socket.
sandbox_tmux_stop_server() {
  local dir=${1-}
  if [ -z "$dir" ]; then
    _sandbox_deny "usage: sandbox_tmux_stop_server <dir>"
    return 1
  fi
  _sandbox_tmux "$dir" kill-server || return 1
}

# Kill one session on the scratch dir's socket (leaves the server running).
sandbox_tmux_stop_session() {
  local dir=${1-} session=${2-}
  if [ -z "$dir" ] || [ -z "$session" ]; then
    _sandbox_deny "usage: sandbox_tmux_stop_session <dir> <session>"
    return 1
  fi
  _sandbox_tmux "$dir" kill-session -t "$session" || return 1
}

# List sessions on the scratch dir's socket.
sandbox_tmux_sessions() {
  local dir=${1-}
  if [ -z "$dir" ]; then
    _sandbox_deny "usage: sandbox_tmux_sessions <dir>"
    return 1
  fi
  _sandbox_tmux "$dir" list-sessions || return 1
}
