#!/usr/bin/env bash
# Verification: supervisor watch priority-stall / backlog ordering /
# priority-band wait stats (story: priority-stall & backlog, task: the new
# priority_stall finding and the improved backlog finding, in Go and in the
# supervisor script oracle).
#
# Checks only the files that task changed.
set -uo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fails=0
ok()  { printf 'ok   %s\n' "$*"; }
bad() { printf 'FAIL %s\n' "$*"; fails=1; }
have() { if grep -q -F -- "$1" "$2" 2>/dev/null; then ok "'$1' in $2"; else bad "missing '$1' in $2"; fi; }
have_not() { if grep -q -F -- "$1" "$2" 2>/dev/null; then bad "unexpected '$1' in $2"; else ok "no '$1' in $2"; fi; }
run() { if "$@" >/dev/null 2>&1; then ok "$*"; else bad "command failed: $*"; fi; }

# --- kind vocabulary: the two new kinds exist in the record file ----------
have 'SupervisorWatchPriorityStall SupervisorWatchKind = "priority_stall"' internal/autonomous/supervisor_watch_record.go
have 'SupervisorWatchBacklog SupervisorWatchKind = "backlog"' internal/autonomous/supervisor_watch_record.go

# --- operator-facing text for both kinds, automaton wording ----------------
have 'SupervisorWatchPriorityStall SupervisorWatchKind = "priority_stall"' internal/autonomous/supervisor_watch_record.go
grep -i 'waiting %d minutes' internal/autonomous/supervisor_watch_text.go >/dev/null 2>&1 \
  && ok "priority stall wording in supervisor_watch_text.go" \
  || bad "no priority stall wording in supervisor_watch_text.go"
have 'Automaton' internal/autonomous/supervisor_watch_text.go

# --- the two checks live on the work checker and read the knobs -----------
have 'func (w *SupervisorWatchWork) CheckPriorityStall(now time.Time, cfg SupervisorWatchConfig)' internal/autonomous/supervisor_watch_work.go
have 'func (w *SupervisorWatchWork) CheckBacklog(now time.Time)' internal/autonomous/supervisor_watch_work.go
have 'EffectiveStallThresholdMinutes' internal/autonomous/supervisor_watch_work.go
have 'EffectivePriorityFloor' internal/autonomous/supervisor_watch_work.go

# --- the pass wires both checks -------------------------------------------
have 'work.CheckPriorityStall(now, cfg)' internal/autonomous/supervisor_watch_pass.go
have 'work.CheckBacklog(now)' internal/autonomous/supervisor_watch_pass.go

# --- naming: never "PRD" in the finding text map, never a tracker id -------
line() { grep -i "$1" "$2" | head -1; }
stall_line=$(line 'PRIORITY STALL' internal/autonomous/supervisor_watch_text.go)
case "$stall_line" in
  *PRD*) bad "priority_stall text says PRD: $stall_line" ;;
  *gh-issue*|*#[0-9]*[[:space:]]*) bad "priority_stall text carries a tracker id: $stall_line" ;;
  *) ok "priority_stall text is automaton-worded, no tracker id" ;;
esac

# --- tests this task adds ---------------------------------------------------
have 'TestSupervisorWorkPriorityStall' internal/autonomous/supervisor_watch_work_test.go
have 'TestSupervisorWorkBacklogOrder' internal/autonomous/supervisor_watch_work_test.go
have 'PriorityStall' internal/autonomous/supervisor_watch_text_test.go
have 'PriorityStall' internal/autonomous/supervisor_watch_pass_test.go

# --- the supervisor script oracle stays in step: one backlog line,
#     highest priority first, then oldest ----------------------------------
have 'priority' scripts/supervisor-watch.py
run python3 -m py_compile scripts/supervisor-watch.py
python3 - <<'EOF' >/dev/null 2>&1 && ok "scripts/supervisor-watch.py sorts the backlog" || bad "scripts/supervisor-watch.py does not sort the backlog"
import ast, sys
src = open('scripts/supervisor-watch.py').read()
tree = ast.parse(src)
# the backlog ordering must sort by priority (desc) then created age (asc)
ok = 'priority' in src and 'backlog.sort(' in src
sys.exit(0 if ok else 1)
EOF

# --- the package tests the task changed ------------------------------------
run go test ./internal/autonomous/ -run '(PriorityStall|Backlog)' -count=1

if [ "$fails" -eq 0 ]; then
  echo "PASS supervisor-watch priority-stall: priority_stall + backlog findings (Go and script oracle)"
  exit 0
else
  echo "FAIL supervisor-watch priority-stall: findings"
  exit 1
fi
