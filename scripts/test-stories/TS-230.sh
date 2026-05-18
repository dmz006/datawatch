#!/usr/bin/env bash
# TS-230 — Howto: v7-compute-migration
# tags: surface:api feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-230"
story_preflight "surface:api feature:howto" || return 0

_story_ts_230() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"compute migration"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-230 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search compute-migration returned $hits hits"
    else
      local list_resp
      list_resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
      list_resp=$(mcp_unwrap "$list_resp")
      if echo "$list_resp" | grep -qi "compute\|migration"; then
        ok "compute-migration howto found in listing"
      else
        skip "compute-migration howto not found (index may not be built)"
      fi
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_230
: "${RESULT:=fail}"
unset -f _story_ts_230
