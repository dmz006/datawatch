#!/usr/bin/env bash
# sandbox.sh — shared helpers for test sandboxes (throwaway daemons, temp dirs,
# private tmux servers).
#
# Sourced by scripts under scripts/ and scripts/test-stories/. Every helper is
# idempotent and safe to call multiple times.
#
# Contract:
#   * sandbox dirs are NEVER inside the repository checkout —
#     sandbox_assert_outside_repo refuses (exit 1) when called with a path
#     inside the checkout, and sandbox_mktemp_dir returns only temp paths
#     outside it.
#   * Daemons are stopped only by the recorded PID (never by name/pattern),
#     SIGTERM first, then SIGKILL after a timeout, then reaped.
#   * Private tmux servers are stopped per-socket (the TMUX_TMPDIR dir the
#     caller assigned), never via a global `tmux kill-server` on the default
#     socket.
#   * sandbox_install_exit_trap chains onto any EXIT trap the caller already
#     had, so it works both standalone and when sourced into a runner that
#     installs its own trap (serial story execution).
#
# Usage (standalone script):
#   source "$(dirname ...)/lib/sandbox.sh"
#   DIR=$(sandbox_mktemp_dir "dw-test")        # outside the checkout, guaranteed
#   sandbox_add_dir "$DIR"
#   datawatch start --foreground --config ... &
#   sandbox_add_pid "$!"
#   sandbox_install_exit_trap                  # stop pid + rm dir on exit/fail/interrupt
#
# Usage (with a private tmux server):
#   TMUX=$(sandbox_mktemp_dir "dw-tmux")
#   sandbox_add_tmux_server "$TMUX"
#   TMUX_TMPDIR="$TMUX" tmux new-session -d -s mybox   # socket lives in $TMUX

# --- repo-root resolution ----------------------------------------------------

# Idempotent source guard: re-sourcing (e.g. a story script inside a runner
# worker shell) must not clobber registrations made earlier in this shell.
if [[ -n "${_SANDBOX_HELPER_LOADED:-}" ]]; then return 0; fi
_SANDBOX_HELPER_LOADED=1

# sandbox_repo_root — print the repository checkout root this sandbox.sh
# copy ships with (best effort).
sandbox_repo_root() {
  # Best effort: git toplevel if resolvable, else the two-level parent of
  # this file (scripts/lib/sandbox.sh -> repo root).
  git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null ||
    (cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
}

# sandbox_abs <path> — print absolute form of a path (no symlink resolution
# needed; the repo check compares lexical paths).
sandbox_abs() {
  case "$1" in
    /*) printf '%s\n' "${1%/}" ;;
    *)  printf '%s\n' "$(pwd)/${1%/}" ;;
  esac
}

# sandbox_assert_outside_repo <dir>
# Exit 1 (refuse) if <dir> is the repo checkout or nested inside it.
sandbox_assert_outside_repo() {
  local dir repo
  dir="$(sandbox_abs "${1:?sandbox_assert_outside_repo: missing dir}")"
  repo="$(sandbox_repo_root 2>/dev/null || true)"
  if [[ -n "$repo" && ( "$dir" == "$repo" || "$dir" == "$repo"/* ) ]]; then
    printf 'sandbox.sh: REFUSING: %s is inside the repository checkout (%s).\n' \
      "$dir" "$repo" >&2
    printf 'sandbox.sh: choose a dir under ${TMPDIR:-/tmp} instead.\n' >&2
    return 1
  fi
  return 0
}

# sandbox_mktemp_dir [prefix] — create a temp dir, guarantee it is not inside
# the checkout (refuse hard), print its path, remove the temp template file
# if the platform left one behind.
sandbox_mktemp_dir() {
  local prefix="${1:-dw-sandbox}" d
  d="$(mktemp -d -t "${prefix}-${RANDOM:-$$}" 2>/dev/null || mktemp -d)" || {
    printf 'sandbox.sh: mktemp -d failed\n' >&2
    return 1
  }
  # mktemp -d never returns a repo path, but assert anyway (defense in depth,
  # incl. when TMPDIR itself is weirdly set by the operator).
  sandbox_assert_outside_repo "$d" || { rm -rf "$d" 2>/dev/null; return 1; }
  printf '%s\n' "$d"
}

# --- PID-scoped daemon teardown ----------------------------------------------
# sandbox_stop_pid <pid> [timeout_s] — SIGTERM, wait, SIGKILL, reap. Only the
# exact PID is ever signaled. 0/missing PID is a no-op.
sandbox_stop_pid() {
  local pid="${1:-}" timeout_s="${2:-15}" waited=0
  [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]] || return 0
  kill -0 "$pid" 2>/dev/null || return 0
  kill "$pid" 2>/dev/null || true
  while kill -0 "$pid" 2>/dev/null; do
    if [[ $waited -ge $timeout_s ]]; then
      kill -9 "$pid" 2>/dev/null || true
      break
    fi
    sleep 1
    waited=$(( waited + 1 ))
  done
  wait "$pid" 2>/dev/null || true
  return 0
}

# --- per-socket tmux teardown --------------------------------------------------
# sandbox_stop_tmux_server <tmux_tmpdir> — kill the tmux server bound to the
# socket under <tmux_tmpdir> only. Uses that TMUX_TMPDIR so no other socket
# (another test, another host session) is ever touched.
sandbox_stop_tmux_server() {
  local tdir="${1:-}"
  [[ -n "$tdir" ]] || return 0
  # $TMUX outranks TMUX_TMPDIR, so inside a tmux pane the old form killed the
  # parent server. Address the throwaway socket explicitly with $TMUX unset,
  # and only when that socket file exists and is not a datawatch production dir.
  local sock="$tdir/tmux-$(id -u)/default"
  [[ -S "$sock" ]] || return 0
  case "$(readlink -f "$tdir")" in
    "$HOME"/.datawatch/tmux*|"$HOME"/.datawatch) return 0 ;;
  esac
  env -u TMUX -u TMUX_TMPDIR tmux -S "$sock" kill-server 2>/dev/null || true
  return 0
}

# --- registration + chained EXIT trap ------------------------------------------
_SANDBOX_PIDS=()
_SANDBOX_DIRS=()
_SANDBOX_TMUX_DIRS=()
_SANDBOX_CLEANED=0
_SANDBOX_TRAP_SET=0

sandbox_add_pid()        { [[ -n "${1:-}" && "${1}" =~ ^[0-9]+$ ]] && _SANDBOX_PIDS+=("$1"); return 0; }
sandbox_add_dir()        { sandbox_assert_outside_repo "${1:?}" 2>/dev/null || return 1; _SANDBOX_DIRS+=("$1"); return 0; }
sandbox_add_tmux_server() { _SANDBOX_TMUX_DIRS+=("${1:?}"); return 0; }

# sandbox_cleanup — stop all registered PIDs, all per-socket tmux servers,
# remove all registered temp dirs (in reverse order), then run any previously
# installed EXIT trap. Idempotent. Preserves the shell's original exit
# status in the handler's return value, and hands it to the chained handler
# as $? (which may itself end the shell with a non-zero status for the
# `FAILED=$?` pattern).
sandbox_cleanup() {
  local rc=$?
  [[ "${_SANDBOX_CLEANED:-0}" == 1 ]] && return 0
  _SANDBOX_CLEANED=1

  # Our own teardown first (the daemon must still be up so API-level
  # cleanup in chained handlers works), then the previously chained trap.
  local i
  for (( i=${#_SANDBOX_PIDS[@]}-1; i>=0; i-- )); do
    sandbox_stop_pid "${_SANDBOX_PIDS[$i]}"
  done
  for (( i=${#_SANDBOX_TMUX_DIRS[@]}-1; i>=0; i-- )); do
    sandbox_stop_tmux_server "${_SANDBOX_TMUX_DIRS[$i]}"
  done
  for (( i=${#_SANDBOX_DIRS[@]}-1; i>=0; i-- )); do
    local d="${_SANDBOX_DIRS[$i]}"
    if sandbox_assert_outside_repo "$d" 2>/dev/null; then
      rm -rf -- "$d" 2>/dev/null || true
    else
      printf 'sandbox.sh: refusing to rm -rf %s (inside repo checkout); leaving it\n' "$d" >&2
    fi
  done

  # Preserve any previously installed EXIT trap (runners install their own
  # cleanup trap before sourcing story scripts). Before running it, restore
  # $? to the status that triggered this trap: my teardown above clobbered
  # it, and chained handlers such as run-tests' `FAILED=$?; cleanup` make
  # keep-or-rm decisions based on that status.
  if [[ -n "${_SANDBOX_PREV_EXIT_TRAP:-}" ]]; then
    if [[ $rc -gt 0 && $rc -lt 256 ]]; then
      return "${rc}"                       # $? is now rc
    fi
    eval "$_SANDBOX_PREV_EXIT_TRAP"
  fi

  # Restore the original exit status: the commands above clobber $?, and
  # bash exits with the last status seen in the handler.
  [[ $rc -eq 0 ]] && return 0
  return $(( rc & 255 ))
}

# sandbox_install_exit_trap — install sandbox_cleanup as the EXIT trap,
# chaining onto whatever was there before. Idempotent.
sandbox_install_exit_trap() {
  [[ "${_SANDBOX_TRAP_SET:-0}" == 1 ]] && return 0
  _SANDBOX_PREV_EXIT_TRAP="$(trap -p EXIT | sed -e 's/^trap -- //' -e 's/ *EXIT$//')"
  trap sandbox_cleanup EXIT
  _SANDBOX_TRAP_SET=1
  return 0
}
