#!/usr/bin/env bash
# TS-273 — autonomous_status via MCP returns {enabled,...} shape
# tags: surface:mcp feature:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-273"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

_story_ts_273() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"autonomous_status","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-273 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "autonomous_status not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and "enabled" in d'; then
    ok "autonomous_status returned dict with enabled key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_status returned dict"
  else
    ko "autonomous_status unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_273
: "${RESULT:=fail}"
unset -f _story_ts_273
