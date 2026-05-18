#!/usr/bin/env bash
# TS-459 — federation_meta_peers MCP tool returns valid JSON
# tags: surface:mcp feature:federation feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-459"
story_preflight "surface:mcp feature:federation feature:observer" || return 0

_story_ts_459() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"federation_meta_peers","params":{}}')
  save_evidence TS-459 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "federation_meta_peers tool returned valid JSON"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "federation_meta_peers tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_459
: "${RESULT:=fail}"
unset -f _story_ts_459
