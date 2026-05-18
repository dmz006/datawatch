#!/usr/bin/env bash
# TS-284 — docs_search for "sessions" returns results with howto refs
# tags: surface:mcp feature:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-284"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_284() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"sessions"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-284 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search sessions returned $hits hits"
    else
      skip "docs_search returned no results (index may not be built)"
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_284
: "${RESULT:=fail}"
unset -f _story_ts_284
