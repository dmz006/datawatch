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

# A "rich" PRD with stories + tasks + verdicts so the expanded card
# shot has substance instead of "no stories yet".
cat >> "$PRD_FILE" <<EOF
{"id":"fixrich","spec":"Add a CACHE column to /api/stats that surfaces RTK cache hit-rate alongside the existing token-savings card","title":"Fixture: rich PRD with stories","project_dir":"/tmp","backend":"claude-code","effort":"normal","status":"running","created_at":"${NOW}","updated_at":"${NOW}","fixture":true,"stories":[{"id":"st1","prd_id":"fixrich","title":"Wire the CACHE column into /api/stats","description":"Surface RTK cache hit-rate in the existing v2 response","status":"in_progress","tasks":[{"id":"t1","story_id":"st1","prd_id":"fixrich","title":"Add CacheHitPct to StatsResponse","spec":"Extend internal/observer/types.go StatsResponse v2 with a CacheHitPct float64 alongside CPUPct.","status":"completed","verification":{"ok":true,"summary":"types.go updated; downstream consumers compile","verified_at":"${NOW}"}},{"id":"t2","story_id":"st1","prd_id":"fixrich","title":"Populate from collector","spec":"Read RTK savings + total tokens, compute hit-rate, set CacheHitPct on the snapshot.","status":"in_progress"},{"id":"t3","story_id":"st1","prd_id":"fixrich","title":"Render the chip","spec":"Add a CACHE % chip to the Settings → Monitor System Statistics panel.","status":"pending","depends_on":["t2"]}],"verdicts":[{"guardrail":"rules","outcome":"pass","summary":"adheres to BL10 v2-additive contract","verdict_at":"${NOW}"}]}],"decisions":[{"at":"${NOW}","kind":"decompose","backend":"claude-code","prompt_chars":820,"response_chars":1140,"actor":"autonomous"},{"at":"${NOW}","kind":"approve","actor":"operator","note":"looks good"},{"at":"${NOW}","kind":"run","actor":"autonomous"}]}
EOF

# One orchestrator graph with two PRD nodes + a guardrail node.
cat >> "$GRAPH_FILE" <<EOF
{"id":"fixgraph0","title":"Fixture graph","project_dir":"/tmp","prd_ids":["fixapproved","fixrunning"],"nodes":[{"id":"n1","graph_id":"fixgraph0","kind":"prd","prd_id":"fixapproved","status":"completed","created_at":"${NOW}","updated_at":"${NOW}"},{"id":"n2","graph_id":"fixgraph0","kind":"guardrail","guardrail":"rules","depends_on":["n1"],"status":"completed","verdict":{"outcome":"pass","summary":"fixture pass","verdict_at":"${NOW}"},"created_at":"${NOW}","updated_at":"${NOW}"}],"status":"completed","created_at":"${NOW}","updated_at":"${NOW}","fixture":true}
EOF

# One pipeline with before/after gates.
cat >> "$PIPE_FILE" <<EOF
{"id":"fixpipe0","name":"Fixture pipeline","tasks":[{"id":"t1","title":"build","spec":"go build","status":"completed"},{"id":"t2","title":"test","spec":"go test","status":"completed","depends_on":["t1"]}],"status":"completed","created_at":"${NOW}","updated_at":"${NOW}","fixture":true}
EOF

echo "[seed] PRD fixtures: $(grep -c '"fixture":true' "$PRD_FILE" 2>/dev/null || echo 0)"
echo "[seed] graph fixtures: $(grep -c '"fixture":true' "$GRAPH_FILE" 2>/dev/null || echo 0)"
echo "[seed] pipeline fixtures: $(grep -c '"fixture":true' "$PIPE_FILE" 2>/dev/null || echo 0)"
echo "[seed] restart the daemon (or POST /api/reload) so it reloads the JSONL stores."
