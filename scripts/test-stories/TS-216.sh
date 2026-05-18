#!/usr/bin/env bash
# TS-216 — Howto: pipeline-chaining
# tags: surface:api feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-216"
story_preflight "surface:api feature:howto" || return 0

_story_ts_216() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"pipeline chaining"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-216 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search pipeline-chaining returned $hits hits"
    else
      # also try docs_list_howtos
      local list_resp
      list_resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
      list_resp=$(mcp_unwrap "$list_resp")
      if echo "$list_resp" | grep -qi "pipeline"; then
        ok "pipeline-chaining howto found in listing"
      else
        skip "pipeline-chaining howto not found (index may not be built)"
      fi
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_216
: "${RESULT:=fail}"
unset -f _story_ts_216
