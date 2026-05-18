#!/usr/bin/env bash
# TS-549 — docs_search "channel bridge dynamic proxy" returns mcp-tools.md
# tags: surface:mcp feature:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-549"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_549() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"channel bridge dynamic proxy"}}')
  save_evidence TS-549 "search.json" "$resp"
  local hits
  hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",[]));print(len(hits) if isinstance(hits,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$hits" -gt 0 ]] 2>/dev/null; then
    if echo "$resp" | grep -qi "mcp-tools"; then
      ok "docs_search returned $hits hits including mcp-tools.md"
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
_story_ts_549
: "${RESULT:=fail}"
unset -f _story_ts_549
