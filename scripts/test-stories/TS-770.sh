#!/usr/bin/env bash
# TS-770 — Quality gates REST round-trip: set + read back per-PRD quality gates
# tags: surface:api feature:autonomous parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-770"

story_preflight "surface:api feature:autonomous" || exit 0

# Create a PRD.
prd_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"spec":"ts770 quality gates test","project_dir":"/tmp","actor":"ts770"}' \
  "$TEST_TLS/api/autonomous/prds" 2>/dev/null)
prd_id=$(echo "$prd_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
if [[ -z "$prd_id" ]]; then
  ko "could not create PRD: $prd_raw"
  exit 0
fi
echo "  [TS-770] PRD=$prd_id"
add_cleanup automaton "$prd_id"

# Set quality gates.
qg_body='{"enabled":true,"test_command":"echo ts770-ok","timeout":30,"block_on_regression":false}'
qg_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "$qg_body" \
  "$TEST_TLS/api/autonomous/prds/$prd_id/set_quality_gates" 2>/dev/null)
qg_id=$(echo "$qg_raw" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',d.get('status','')))" 2>/dev/null)
echo "  [TS-770] set_quality_gates response id/status: $qg_id"
save_evidence "$CURRENT_STORY" "set_qg.json" "$qg_raw"

# Read back PRD and verify quality gates persisted.
prd_detail=$(curl "${curl_args[@]}" "$TEST_TLS/api/autonomous/prds/$prd_id" 2>/dev/null)
qg_enabled=$(echo "$prd_detail" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('quality_gates',{}).get('enabled','?'))" 2>/dev/null)
qg_cmd=$(echo "$prd_detail" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('quality_gates',{}).get('test_command','?'))" 2>/dev/null)
qg_timeout=$(echo "$prd_detail" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('quality_gates',{}).get('timeout','?'))" 2>/dev/null)
echo "  [TS-770] read-back: enabled=$qg_enabled cmd=$qg_cmd timeout=$qg_timeout"
save_evidence "$CURRENT_STORY" "prd.json" "$prd_detail"

if [[ "${qg_enabled,,}" != "true" ]]; then
  ko "quality_gates.enabled not persisted: got $qg_enabled"
  exit 0
fi
if [[ "$qg_cmd" != "echo ts770-ok" ]]; then
  ko "quality_gates.test_command not persisted: got $qg_cmd"
  exit 0
fi
if [[ "$qg_timeout" != "30" ]]; then
  ko "quality_gates.timeout not persisted: got $qg_timeout"
  exit 0
fi

# Also check default_quality_gates via global config (separate from per-PRD).
dflt=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null | \
  python3 -c "import sys,json; c=json.load(sys.stdin); d=c.get('autonomous',{}).get('default_quality_gates',{}); print(d.get('enabled','?'))" 2>/dev/null)
echo "  [TS-770] global default_quality_gates.enabled=$dflt"

ok "quality gates persisted on PRD $prd_id: enabled=$qg_enabled cmd=$qg_cmd timeout=$qg_timeout"
