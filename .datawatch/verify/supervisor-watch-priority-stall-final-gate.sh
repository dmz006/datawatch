#!/usr/bin/env bash
# Verification: supervisor watch priority-stall / backlog ordering /
# priority-band wait stats (story: docs & gates, task: final gate — a
# read-only coverage pass over every surface of the feature).
#
# This gate changes nothing: it re-runs the per-task scripts, builds the
# affected packages, checks cross-surface parity (config keys, finding
# kinds, band stats, locales), and checks that the coverage report exists.
set -uo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fails=0
ok()  { printf 'ok   %s\n' "$*"; }
bad() { printf 'FAIL %s\n' "$*"; fails=1; }
have() { if grep -q -F -- "$1" "$2" 2>/dev/null; then ok "'$1' in $2"; else bad "missing '$1' in $2"; fi; }
run() { if "$@" >/dev/null 2>&1; then ok "$*"; else bad "command failed: $*"; fi; }

REPORT=.datawatch/reports/priority-stall-coverage-final-gate.md

# --- the per-task gates still pass -------------------------------------------
run bash .datawatch/verify/supervisor-watch-priority-stall-config.sh
run bash .datawatch/verify/supervisor-watch-priority-stall-findings.sh
run bash .datawatch/verify/supervisor-watch-priority-stall-bands.sh
run bash .datawatch/verify/supervisor-watch-priority-stall-docs.sh

# --- the code still builds and the touched packages vet clean ----------------
run go build ./internal/autonomous/ ./internal/config/ ./internal/mcp/ ./internal/server/ ./cmd/datawatch/
run go vet ./internal/autonomous/

# --- parity: the same vocabulary on every surface ---------------------------
# config keys: the Go type, the server surface, the PWA, the docs
for needle in stall_threshold_minutes priority_floor; do
  have "$needle" internal/config/config.go
  have "$needle" internal/server/api.go
  have "$needle" internal/server/web/app.js
  have "$needle" docs/config-reference.yaml
done
for loc in en de es fr ja; do
  have 'settings_supervisor_watch_stall_threshold_minutes' "internal/server/web/locales/$loc.json"
  have 'settings_supervisor_watch_priority_floor' "internal/server/web/locales/$loc.json"
done

# finding kinds: the record constant, the text map, the howto, the script oracle
for needle in priority_stall; do
  have "$needle" internal/autonomous/supervisor_watch_record.go
  have "PriorityStall" internal/autonomous/supervisor_watch_text.go
  have "$needle" docs/howto/automation-supervisor-watch.md
done
have 'BACKLOG' scripts/supervisor-watch.py

# band stats: the shared body, the MCP tool test, the REST body test, the PWA card
have 'PriorityBands' internal/autonomous/supervisor_watch_status.go
have 'PriorityBands' internal/mcp/supervisor_watch_tools_test.go
have 'PriorityBands' internal/server/supervisor_watch_test.go
have 'priority_bands' internal/server/web/app.js

# --- the coverage report this gate produces ----------------------------------
[ -f "$REPORT" ] && ok "report exists: $REPORT" || bad "report missing: $REPORT"
if [ -f "$REPORT" ]; then
  for needle in "## Surfaces" "## Evidence matrix" "## Discrepancies" "## Gaps" "## Gate outcome"; do
    have "$needle" "$REPORT"
  done
fi

if [ "$fails" -eq 0 ]; then
  echo "PASS supervisor-watch priority-stall: final gate (all surfaces, build, parity, report)"
  exit 0
else
  echo "FAIL supervisor-watch priority-stall: final gate"
  exit 1
fi
