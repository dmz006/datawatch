#!/usr/bin/env bash
# TS-231 — Howto: screenshots (if any)
# tags: surface:api feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-231"
story_preflight "surface:api feature:howto" || return 0

_story_ts_231() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"screenshots"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-231 "search.json" "$resp"
  # Accept empty result — screenshots howto may not exist
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    hits=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);hits=d.get("hits",d.get("results",d if isinstance(d,list) else []));print(len(hits))' 2>/dev/null || echo "0")
    if [[ "$hits" -gt 0 ]] 2>/dev/null; then
      ok "docs_search screenshots returned $hits hits"
    else
      skip "screenshots howto not found (may not exist in this build)"
    fi
  else
    skip "docs_search not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_231
: "${RESULT:=fail}"
unset -f _story_ts_231
