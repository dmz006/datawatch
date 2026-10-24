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
rm_init_root # one work root shared by every states-*.sh script below

status=0
for states in "$RM_ENTRY_DIR"/restart-matrix/states-*.sh; do
  [[ -f "$states" ]] || continue
  bash "$states" "$@"
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
