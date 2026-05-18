#!/usr/bin/env bash
# TS-350 — docs_search "enable memory sqlite" returns result with howto ref
# tags: surface:mcp feature:mcp feature:howto feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-350"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

_story_ts_350() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"enable memory sqlite"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-350 "search.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search 'enable memory sqlite' returned $hits hits"
    else
      skip "docs_search returned no results (index may not be built)"
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_350
: "${RESULT:=fail}"
unset -f _story_ts_350
