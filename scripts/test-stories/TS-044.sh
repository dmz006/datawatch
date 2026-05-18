#!/usr/bin/env bash
# TS-044 — KG add triple
# tags: surface:api feature:kg
# legacy fn: t5_ts044_kg_add_triple
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-044"
story_preflight "surface:api feature:kg" || return 0

_story_ts_044() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(api POST /api/memory/kg/add '{"subject":"test-entity-e2e-'"$$"'","predicate":"is_a","object":"test-object-e2e"}')
  save_evidence TS-044 "add.json" "$resp"
  KG_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$KG_ID" && "$KG_ID" != "0" ]]; then
    add_cleanup kg "$KG_ID"
    ok "KG triple added: id=$KG_ID"
  else
    ko "KG add failed: $resp"
  fi
}

RESULT=fail
_story_ts_044
: "${RESULT:=fail}"
unset -f _story_ts_044
