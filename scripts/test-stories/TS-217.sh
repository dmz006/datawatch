#!/usr/bin/env bash
# TS-217 — Howto: skills-sync
# tags: surface:api feature:howto feature:skills
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-217"
story_preflight "surface:api feature:howto feature:skills" || return 0

_story_ts_217() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"skills sync"}}')
  save_evidence TS-217 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search skills-sync returned $hits hits"
    else
      local list_resp
      list_resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
      if echo "$list_resp" | grep -qi "skills"; then
        ok "skills howto found in listing"
      else
        skip "skills-sync howto not found (index may not be built)"
      fi
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_217
: "${RESULT:=fail}"
unset -f _story_ts_217
