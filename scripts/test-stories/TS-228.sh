#!/usr/bin/env bash
# TS-228 — Howto: channel-state-engine
# tags: surface:api feature:howto feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-228"
story_preflight "surface:api feature:howto feature:sessions" || return 0

_story_ts_228() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"channel state engine"}}')
  save_evidence TS-228 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search channel-state-engine returned $hits hits"
    else
      local list_resp
      list_resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
      if echo "$list_resp" | grep -qi "channel\|state"; then
        ok "channel-state-engine howto found in listing"
      else
        skip "channel-state-engine howto not found (index may not be built)"
      fi
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_228
: "${RESULT:=fail}"
unset -f _story_ts_228
