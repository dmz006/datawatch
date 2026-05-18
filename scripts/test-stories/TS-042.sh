#!/usr/bin/env bash
# TS-042 — Memory list
# tags: surface:api feature:memory
# legacy fn: t5_ts042_memory_list
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-042"
story_preflight "surface:api feature:memory" || return 0

_story_ts_042() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(curl "${curl_args[@]}" "$TEST_BASE/api/memory/list?limit=50")
  save_evidence TS-042 "list.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/memory/list returns array"
    if [[ -n "$MEM_ID" && "$MEM_ID" != "0" ]]; then
      if echo "$resp" | python3 -c "import json,sys; arr=json.load(sys.stdin); assert any(int(m.get('id',0))==$MEM_ID for m in arr)" 2>/dev/null; then
        ok "saved memory id=$MEM_ID found in list"
      else
        ko "saved memory id=$MEM_ID NOT in list"
      fi
    fi
  else
    ko "memory list shape wrong: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_042
: "${RESULT:=fail}"
unset -f _story_ts_042
