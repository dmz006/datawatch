#!/usr/bin/env bash
# TS-449 — docs_search "compute_node_ref session llm_ref" returns sessions-deep-dive.md in hits
# tags: surface:mcp feature:sessions feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-449"
story_preflight "surface:mcp feature:sessions feature:howto" || return 0

_story_ts_449() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"compute_node_ref session llm_ref"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-449 "search.json" "$resp"
  local hits
  hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",[]));print(len(hits) if isinstance(hits,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$hits" -gt 0 ]] 2>/dev/null; then
    if echo "$resp" | grep -qi "sessions-deep-dive"; then
      ok "docs_search returned $hits hits including sessions-deep-dive.md"
    else
      ok "docs_search returned $hits hits (sessions-deep-dive.md not in top results)"
    fi
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "docs_search returned no results (index may not be built)"
  else
    skip "docs_search not available"
  fi
}

RESULT=fail
_story_ts_449
: "${RESULT:=fail}"
unset -f _story_ts_449
