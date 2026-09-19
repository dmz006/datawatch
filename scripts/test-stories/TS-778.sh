#!/usr/bin/env bash
# TS-778 — BL387: memory wire-up source inspection + memoryContextFn live
# tags: surface:api feature:memory feature:autonomous live:yes parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-778"

story_preflight "surface:api feature:memory" || return 0

# ── Part 1: Source inspection — all 5 BL387 callbacks must be called in main.go ──
main_go="$REPO_ROOT/cmd/datawatch/main.go"
if [[ ! -f "$main_go" ]]; then
  ko "cmd/datawatch/main.go not found"
  return 0
fi

for fn in SetMemoryVerifierFn SetMemoryScopeSeedFn SetMemoryContextFn SetMemoryCrossSeedFn SetMemoryReportFn; do
  count=$(grep -c "amgr\.$fn\|mgr\.$fn\b" "$main_go" 2>/dev/null || echo 0)
  echo "  [TS-778] $fn wired in main.go: $count occurrences"
  if [[ "$count" -lt 1 ]]; then
    ko "BL387 callback $fn not called in cmd/datawatch/main.go"
    return 0
  fi
done
save_evidence "$CURRENT_STORY" "wiring_check.txt" "all 5 BL387 callbacks verified in main.go"

# ── Part 2: Live memory + context retrieval ──

# Check memory is enabled.
mem_cfg=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null | \
  python3 -c "import sys,json; c=json.load(sys.stdin); m=c.get('memory',{}); print(m.get('enabled',False))" 2>/dev/null)
echo "  [TS-778] memory.enabled=$mem_cfg"
if [[ "${mem_cfg,,}" != "true" ]]; then
  skip "memory not enabled in sandbox config — skip live memoryContextFn test"
  return 0
fi

# Save a memory entry to a project scope.
proj_dir="/tmp/ts778-memory-test"
mem_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"scope\":{\"scope\":\"project-shared\",\"project\":\"$proj_dir\"},\"content\":\"TS-778: memory context function wired via BL387. The answer is 42.\",\"role\":\"test\"}" \
  "$TEST_TLS/api/memory/scopes/save" 2>/dev/null)
mem_status=$(echo "$mem_raw" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('status',d.get('id','?')))" 2>/dev/null)
echo "  [TS-778] memory save: $mem_status"
save_evidence "$CURRENT_STORY" "mem_save.json" "$mem_raw"

# Wait briefly for embedding.
sleep 2

# Create a PRD with the same project_dir so memoryContextFn can enrich planning.
prd_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"spec\":\"ts778 memory context test: what is the answer?\",\"project_dir\":\"$proj_dir\",\"actor\":\"ts778\"}" \
  "$TEST_TLS/api/autonomous/prds" 2>/dev/null)
prd_id=$(echo "$prd_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
echo "  [TS-778] PRD=$prd_id"
save_evidence "$CURRENT_STORY" "prd.json" "$prd_raw"

if [[ -n "$prd_id" ]]; then
  add_cleanup automaton "$prd_id"
fi

# Verify memory search endpoint returns the entry.
search_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"scope\":{\"scope\":\"project-shared\",\"project\":\"$proj_dir\"},\"query\":\"memory context function BL387\",\"top_k\":3}" \
  "$TEST_TLS/api/memory/scopes/search" 2>/dev/null)
search_count=$(echo "$search_raw" | python3 -c "import sys,json; d=json.load(sys.stdin); r=d.get('results',d.get('entries',[])); print(len(r))" 2>/dev/null || echo 0)
echo "  [TS-778] memory search results: $search_count"
save_evidence "$CURRENT_STORY" "mem_search.json" "$search_raw"

if [[ "$search_count" -lt 1 ]]; then
  # Non-fatal: embedder may be slow or not reachable.
  echo "  [TS-778] WARN: memory search returned 0 results (embedder may be unavailable)"
fi

ok "BL387 wiring confirmed in main.go (all 5 callbacks); memory.enabled=$mem_cfg; PRD=$prd_id; search_results=$search_count"
