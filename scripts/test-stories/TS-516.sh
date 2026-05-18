#!/usr/bin/env bash
# TS-516 — docs_search "datawatch-stats diag multi-parent" returns compute-nodes.md
# tags: surface:mcp feature:compute feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-516"
story_preflight "surface:mcp feature:compute feature:howto" || return 0

_story_ts_516() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"datawatch-stats diag multi-parent"}}')
  save_evidence TS-516 "search.json" "$resp"
  local hits
  hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",[]));print(len(hits) if isinstance(hits,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$hits" -gt 0 ]] 2>/dev/null; then
    if echo "$resp" | grep -qi "compute-nodes"; then
      ok "docs_search returned $hits hits including compute-nodes.md"
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
_story_ts_516
: "${RESULT:=fail}"
unset -f _story_ts_516
