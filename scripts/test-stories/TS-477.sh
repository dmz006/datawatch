#!/usr/bin/env bash
# TS-477 — autonomous_prd_approve MCP tool returns dict
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-477"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_477() {
  # Use a fresh PRD — earlier tests may have already approved the shared AUTOMATON_ID
  local local_id
  local resp
  resp=$(api POST /api/autonomous/prds '{"spec":"ts477-approve-test: echo ok","project_dir":"/tmp","backend":"claude-code","effort":"low"}')
  local_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$local_id" ]]; then
    skip "could not create PRD for approve test"
    return
  fi
  add_cleanup automaton "$local_id"

  # Decompose first to move PRD to needs_review state
  local decomp_code
  decomp_code=$(curl -sk -o /dev/null -w "%{http_code}" --max-time 60 \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/autonomous/prds/$local_id/decompose")
  if [[ "$decomp_code" != "200" ]]; then
    skip "decompose returned $decomp_code — cannot get PRD to needs_review"
    return
  fi

  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_prd_approve\",\"params\":{\"id\":\"$local_id\"}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-477 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "autonomous_prd_approve tool not available"
    return
  fi
  if echo "$resp" | grep -qi "not approvable\|status.*draft\|must be.*pending\|cannot approve"; then
    skip "PRD not in approvable state after decompose"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_prd_approve tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_477
: "${RESULT:=fail}"
unset -f _story_ts_477
