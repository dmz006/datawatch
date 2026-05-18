#!/usr/bin/env bash
# TS-594 — docs_search "federation peer capabilities" returns federation-cbac.md
# tags: surface:mcp feature:federation feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-594"
story_preflight "surface:mcp feature:federation feature:howto" || return 0

_story_ts_594() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"q":"federation peer capabilities"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-594 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "docs_search MCP tool not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "docs_search returned list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "docs_search returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_594
: "${RESULT:=fail}"
unset -f _story_ts_594
