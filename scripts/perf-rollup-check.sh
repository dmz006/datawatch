#!/usr/bin/env bash
# scripts/perf-rollup-check.sh — roll-up and Automata list performance guard
# at 700 Automata.
#
# Runs, against a deterministic temp-dir fixture (no network, no git, never the
# operator's data directory):
#   1. go test ./internal/server/ -run 'TestPerfRollup|TestPerfAutomata'
#      roll-up warm < 300 ms, cold < 1.5 s, list < 300 ms and < 2 MB, a write
#      invalidates, and no full record is read by the roll-up or the list.
#   2. go test ./internal/autonomous/ -run 'TestSummaryIndexMemory'
#      per-entry and total byte bound, and bounded heap after 5,000 refreshes.
#
# Environment:
#   PERF_ROLLUP_CHECK       set to 1 by this script; the time-limit assertions are
#                           only enforced when it is 1. Byte limits always run.
#   PERF_ROLLUP_MULTIPLIER  number >= 1, default 1. Scales every time limit for
#                           a slow CI host (3 allows 900 ms warm, 4.5 s cold).
#                           The byte limits never scale.
#   PERF_ROLLUP_TIMEOUT     hard timeout in seconds for each go test, default 600.
#
# The tests run niced (nice -n 19), with workers capped at a quarter of the
# cores, under a hard timeout. There is no load or soak test here. Exits
# non-zero on any miss.
set -u

cd "$(dirname "$0")/.." || exit 1

GO=$(command -v go) || { echo "perf-rollup-check: go not found" >&2; exit 2; }

MULT="${PERF_ROLLUP_MULTIPLIER:-1}"
case "$MULT" in
  ''|*[!0-9.]*|*.*.*|.) echo "perf-rollup-check: PERF_ROLLUP_MULTIPLIER must be a number >= 1, got '$MULT'" >&2; exit 2 ;;
esac
if ! awk -v m="$MULT" 'BEGIN { exit !(m + 0 >= 1) }'; then
  echo "perf-rollup-check: PERF_ROLLUP_MULTIPLIER must be a number >= 1, got '$MULT'" >&2
  exit 2
fi
export PERF_ROLLUP_MULTIPLIER="$MULT"
# Opt in to the host-speed time limits (skipped in a normal go test run).
export PERF_ROLLUP_CHECK=1

LIMIT="${PERF_ROLLUP_TIMEOUT:-600}"
case "$LIMIT" in ''|*[!0-9]*) echo "perf-rollup-check: PERF_ROLLUP_TIMEOUT must be an integer, got '$LIMIT'" >&2; exit 2 ;; esac

CORES=$(nproc 2>/dev/null || echo 4)
WORKERS=$(( CORES / 4 ))
[ "$WORKERS" -ge 1 ] || WORKERS=1
export GOMAXPROCS="$WORKERS"

# Never let a test reach an operator multiplexer from this script's shell.
unset TMUX

LOG=$(mktemp) || exit 2
cleanup() { rm -f "$LOG"; }
trap cleanup EXIT

run() {
  # run <label> <go test args...>: niced, capped, under a hard timeout.
  label=$1; shift
  echo "== $label =="
  if ! nice -n 19 timeout --kill-after=15 "$LIMIT" "$GO" test -count=1 -p "$WORKERS" "$@" >"$LOG" 2>&1; then
    cat "$LOG" >&2
    echo "perf-rollup-check: $label FAILED" >&2
    return 1
  fi
  cat "$LOG"
  return 0
}

fail=0
run "roll-up and Automata list at 700 Automata" ./internal/server/ -run 'TestPerfRollup|TestPerfAutomata' || fail=1
run "summary index memory" ./internal/autonomous/ -run 'TestSummaryIndexMemory' || fail=1

if [ "$fail" -ne 0 ]; then echo "perf-rollup-check: FAILED" >&2; exit 1; fi
echo "perf-rollup-check: OK"
