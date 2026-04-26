#!/usr/bin/env bash
# BL190 (v5.11.0+) — seed JSONL fixtures for the howto screenshot suite.
#
# Idempotent: each run wipes anything tagged `fixture: true` and
# re-seeds. Non-fixture data is left alone so an operator running this
# on a populated workstation doesn't lose their PRDs / orchestrator
# graphs / pipeline jobs.
#
# Usage:
#   bash scripts/howto-seed-fixtures.sh [--data-dir=$HOME/.datawatch]
#
# After running, restart the daemon (or POST /api/reload) so it picks
# up the seeded JSONL stores; otherwise the in-memory cache wins.

set -euo pipefail

DATA_DIR="${1:-${HOME}/.datawatch}"
DATA_DIR="${DATA_DIR#--data-dir=}"

PRD_FILE="${DATA_DIR}/autonomous/prds.jsonl"
GRAPH_FILE="${DATA_DIR}/orchestrator/graphs.jsonl"
PIPE_FILE="${DATA_DIR}/pipeline/jobs.jsonl"

mkdir -p "${DATA_DIR}/autonomous" "${DATA_DIR}/orchestrator" "${DATA_DIR}/pipeline"

# Wipe prior fixtures (lines containing "\"fixture\":true") then re-seed.
strip_fixtures() {
  local f="$1"
  if [[ -f "$f" ]]; then
    grep -v '"fixture":true' "$f" > "${f}.tmp" || true
    mv "${f}.tmp" "$f"
  fi
}

strip_fixtures "$PRD_FILE"
strip_fixtures "$GRAPH_FILE"
strip_fixtures "$PIPE_FILE"

NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# One PRD per status pill so the screenshot covers every badge.
for s in draft decomposing needs_review approved running completed; do
  cat >> "$PRD_FILE" <<EOF
{"id":"fix${s}","spec":"Fixture PRD — ${s}","title":"Fixture: ${s}","project_dir":"/tmp","backend":"claude-code","effort":"normal","status":"${s}","created_at":"${NOW}","updated_at":"${NOW}","fixture":true}
EOF
done

# One orchestrator graph with two PRD nodes + a guardrail node.
cat >> "$GRAPH_FILE" <<EOF
{"id":"fixgraph0","title":"Fixture graph","project_dir":"/tmp","prd_ids":["fixapproved","fixrunning"],"nodes":[{"id":"n1","graph_id":"fixgraph0","kind":"prd","prd_id":"fixapproved","status":"completed","created_at":"${NOW}","updated_at":"${NOW}"},{"id":"n2","graph_id":"fixgraph0","kind":"guardrail","guardrail":"rules","depends_on":["n1"],"status":"completed","verdict":{"outcome":"pass","summary":"fixture pass","verdict_at":"${NOW}"},"created_at":"${NOW}","updated_at":"${NOW}"}],"status":"completed","created_at":"${NOW}","updated_at":"${NOW}","fixture":true}
EOF

# One pipeline with before/after gates.
cat >> "$PIPE_FILE" <<EOF
{"id":"fixpipe0","name":"Fixture pipeline","tasks":[{"id":"t1","title":"build","spec":"go build","status":"completed"},{"id":"t2","title":"test","spec":"go test","status":"completed","depends_on":["t1"]}],"status":"completed","created_at":"${NOW}","updated_at":"${NOW}","fixture":true}
EOF

echo "[seed] PRD fixtures: $(grep -c fixture: "$PRD_FILE" 2>/dev/null || echo 0)"
echo "[seed] graph fixtures: $(grep -c fixture: "$GRAPH_FILE" 2>/dev/null || echo 0)"
echo "[seed] pipeline fixtures: $(grep -c fixture: "$PIPE_FILE" 2>/dev/null || echo 0)"
echo "[seed] restart the daemon (or POST /api/reload) so it reloads the JSONL stores."
