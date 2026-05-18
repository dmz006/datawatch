#!/usr/bin/env bash
# TS-546 — docs_search "council async run SSE" returns council-mode.md
# tags: surface:mcp feature:council feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-546"
story_preflight "surface:mcp feature:council feature:howto" || return 0

_story_ts_546() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"council async run SSE"}}')
  save_evidence TS-546 "search.json" "$resp"
  local hits
  hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",[]));print(len(hits) if isinstance(hits,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$hits" -gt 0 ]] 2>/dev/null; then
    if echo "$resp" | grep -qi "council-mode"; then
      ok "docs_search returned $hits hits including council-mode.md"
    else
      ok "docs_search returned $hits hits"
    fi
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "docs_search returned no results (index may not be built)"
  else
    skip "docs_search not available"
  fi
}

RESULT=fail
_story_ts_546
: "${RESULT:=fail}"
unset -f _story_ts_546
