#!/usr/bin/env bash
# TS-045 — KG query entity
# tags: surface:api feature:kg
# legacy fn: t5_ts045_kg_query
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-045"
story_preflight "surface:api feature:kg" || return 0

_story_ts_045() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  if [[ -z "$KG_ID" ]]; then skip "no KG ID (TS-044 failed)"; return; fi
  local resp
  resp=$(curl "${curl_args[@]}" "$TEST_BASE/api/memory/kg/query?entity=test-entity-e2e-$$")
  save_evidence TS-045 "query.json" "$resp"
  if echo "$resp" | python3 -c "import json,sys; arr=json.load(sys.stdin); assert any(int(t.get('id',0))==$KG_ID for t in arr)" 2>/dev/null; then
    ok "KG query returned id=$KG_ID"
  else
    ko "KG query did not return id=$KG_ID: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_045
: "${RESULT:=fail}"
unset -f _story_ts_045
