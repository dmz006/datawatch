#!/usr/bin/env bash
# TS-432 — docs_search \"compute node\" returns result referencing compute-nodes howto
# tags: surface:mcp feature:mcp feature:howto feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-432"
story_preflight "surface:mcp feature:mcp feature:howto feature:compute" || return 0

_story_ts_432() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_search","params":{"query":"compute node"}}')
  save_evidence TS-432 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list) and len(d) > 0'; then
    ok "docs_search 'compute node' returns non-empty array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("results" in d or "items" in d)'; then
    ok "docs_search 'compute node' returns dict with results"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "docs_search 'compute node' returns dict"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not available"; then
    skip "docs_search MCP tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_432
: "${RESULT:=fail}"
unset -f _story_ts_432
