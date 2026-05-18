#!/usr/bin/env bash
# TS-043 — Memory delete
# tags: surface:api feature:memory conflict:db-write
# legacy fn: t5_ts043_memory_delete
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-043"
story_preflight "surface:api feature:memory conflict:db-write" || return 0

_story_ts_043() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  if [[ -z "$MEM_ID" || "$MEM_ID" == "0" ]]; then skip "no memory ID to delete"; return; fi
  local resp
  resp=$(api POST /api/memory/delete '{"id":'"$MEM_ID"'}')
  save_evidence TS-043 "delete.json" "$resp"
  if assert_json "$resp" '"status" in d'; then
    ok "memory id=$MEM_ID deleted"
    MEM_ID=""
  else
    ko "memory delete failed: $resp"
  fi
}

RESULT=fail
_story_ts_043
: "${RESULT:=fail}"
unset -f _story_ts_043
