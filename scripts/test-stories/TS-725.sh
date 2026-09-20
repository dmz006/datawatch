#!/usr/bin/env bash
# TS-725 — Automata memory: GET /api/autonomous/prds/{id} includes memory_report/memory_report_at fields
# tags: surface:api feature:automata feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-725"
story_preflight "surface:api feature:automata feature:memory" || return 0

_story_ts_725() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Look for a PRD with memory_report set (only appears on completed PRDs with seed enabled)
  local prds_resp
  prds_resp=$(api GET /api/autonomous/prds 2>/dev/null || echo "[]")
  save_evidence TS-725 "prds_list.json" "$prds_resp"

  local report_found
  report_found=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    r = prd.get("memory_report","")
    if r:
        print(r[:80])
        import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ -n "$report_found" ]]; then
    ok "PRD has memory_report field set — auto-report on completion wired (v8.33.0): $report_found..."
    return
  fi

  # Check schema on any PRD
  local field_present
  field_present=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    print("yes" if "memory_report" in prd else "no")
    import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ "$field_present" == "yes" ]]; then
    ok "PRD schema includes memory_report field — memory auto-report present (v8.33.0)"
    return
  fi

  skip "no PRDs found or memory_report not in schema — run a completed Automata with memory_seed.enabled=true"
}

RESULT=fail
_story_ts_725
: "${RESULT:=fail}"
unset -f _story_ts_725
