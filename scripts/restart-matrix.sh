#!/usr/bin/env bash
# scripts/restart-matrix.sh — entry point for the daemon-restart matrix.
#
# Restarts a SANDBOX daemon while autonomous work is in flight and records what
# became of that work, state by state. Every state runs against its own throwaway
# daemon: generated config, its own data dir, free ports, TLS off, and its own
# multiplexer socket under a temp dir. The production daemon, its ports and its
# data dir are never touched; the run refuses to start if a target matches them.
#
# Usage:
#   scripts/restart-matrix.sh [--strict] [--only NAME[,NAME]] [--list] [--keep]
#
#   --strict   exit 1 when any state fails (default is measure mode: exit 0)
#   --only     run just the named states (see --list)
#   --list     print the state names and exit
#   --keep     keep each sandbox dir after its state (records are always kept)
#
# Environment:
#   DATAWATCH_BIN   daemon binary (default: ./datawatch, build it with
#                   'go build -o datawatch ./cmd/datawatch/')
#   RM_TMP_BASE     parent of the throwaway work root (default: $TMPDIR or /tmp);
#                   keep it short, the multiplexer socket path has a length limit
#
# Output: PASS or FAIL per criterion on stdout, one JSON record per state and a
# combined summary.json under <work root>/records. Exit status: 0 measured (or
# strict and clean), 1 strict and a state failed, 2 harness fault.

set -uo pipefail
unset TMUX TMUX_TMPDIR # an ambient multiplexer never leaks into the sandbox

RM_ENTRY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=restart-matrix/lib.sh
. "$RM_ENTRY_DIR/restart-matrix/lib.sh"

for arg in "$@"; do
  case "$arg" in
    -h | --help)
      sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed -E 's/^# ?//'
      exit 0
      ;;
    --list)
      # no work root is created just to print names
      for states in "$RM_ENTRY_DIR"/restart-matrix/states-*.sh; do
        [[ -f "$states" ]] && bash "$states" --list
      done
      exit 0
      ;;
  esac
done

rm_require_tools

# --only is split here and each state file gets only the names it owns, so a
# name from one file never reaches another file's parser. A name no file knows
# is a usage error, caught before any sandbox is made.
only=""
pass=()
while (($#)); do
  case "$1" in
    --only) only="${2:-}"; shift || true; shift || true ;;
    --only=*) only="${1#--only=}"; shift ;;
    *) pass+=("$1"); shift ;;
  esac
done
requested=()
[[ -n "$only" ]] && IFS=, read -r -a requested <<<"$only"

files=()
file_names=()
for states in "$RM_ENTRY_DIR"/restart-matrix/states-*.sh; do
  [[ -f "$states" ]] || continue
  files+=("$states")
  file_names+=("$(bash "$states" --list | paste -sd, -)")
done
for r in "${requested[@]}"; do
  found=0
  for names in "${file_names[@]}"; do [[ ",$names," == *",$r,"* ]] && found=1; done
  ((found)) || rm_die "unknown state: $r (see --list)"
done

rm_init_root # one work root shared by every states-*.sh script below

status=0
for i in "${!files[@]}"; do
  args=("${pass[@]}")
  if ((${#requested[@]})); then
    subset=()
    for r in "${requested[@]}"; do [[ ",${file_names[i]}," == *",$r,"* ]] && subset+=("$r"); done
    ((${#subset[@]})) || continue
    args+=(--only "$(IFS=,; echo "${subset[*]}")")
  fi
  bash "${files[i]}" "${args[@]}"
  rc=$?
  case $rc in
    0) ;;
    1) status=1 ;;
    *) exit "$rc" ;; # harness fault: stop, do not mask it
  esac
done

rm_write_summary
printf '\nrecords: %s\n' "$RM_REC"
exit "$status"
