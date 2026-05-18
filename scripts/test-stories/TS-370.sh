#!/usr/bin/env bash
# TS-370 — DELETE /api/autonomous/guardrail-profiles/{id} returns 200
# tags: surface:api feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-370"
story_preflight "surface:api feature:automata" || return 0

_story_ts_370() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled — skip guardrail profile delete test"
    return
  fi
  # Create a profile to delete
  local create_resp pid
  create_resp=$(api POST /api/autonomous/guardrail-profiles \
    '{"name":"test-gp-delete","rules":[]}')
  pid=$(echo "$create_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$pid" ]]; then
    skip "could not create guardrail profile for delete test: $(echo "$create_resp" | head -c 100)"
    return
  fi
  # DELETE it
  local del_code
  del_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X DELETE \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/autonomous/guardrail-profiles/$pid")
  save_evidence TS-370 "delete_code.txt" "$del_code"
  if [[ "$del_code" == "200" || "$del_code" == "204" ]]; then
    ok "DELETE /api/autonomous/guardrail-profiles/$pid returned $del_code"
  elif [[ "$del_code" == "404" ]]; then
    ko "profile $pid not found on DELETE"
  else
    ko "unexpected HTTP $del_code on DELETE"
  fi
}

RESULT=fail
_story_ts_370
: "${RESULT:=fail}"
unset -f _story_ts_370
