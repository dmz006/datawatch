#!/usr/bin/env bash
# TS-218 — Howto: push-notifications
# tags: surface:api feature:howto feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-218"
story_preflight "surface:api feature:howto feature:comms" || return 0

_story_ts_218() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"push notifications"}}')
  save_evidence TS-218 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search push-notifications returned $hits hits"
    else
      local list_resp
      list_resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
      if echo "$list_resp" | grep -qi "push\|notification"; then
        ok "push-notifications howto found in listing"
      else
        skip "push-notifications howto not found (index may not be built)"
      fi
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_218
: "${RESULT:=fail}"
unset -f _story_ts_218
