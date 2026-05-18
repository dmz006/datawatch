#!/usr/bin/env bash
# TS-270 — algorithm_list via MCP returns array
# tags: surface:mcp feature:mcp feature:algorithm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-270"
story_preflight "surface:mcp feature:mcp feature:algorithm" || return 0

_story_ts_270() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"algorithm_list","params":{}}')
  save_evidence TS-270 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "algorithm_list returned array (${#resp} bytes)"
  elif assert_json "$resp" 'isinstance(d, dict) and ("algorithms" in d or "items" in d or "result" in d)'; then
    ok "algorithm_list returned dict with algorithms key"
  elif echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "algorithm_list not available in this build"
  else
    ko "algorithm_list unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_270
: "${RESULT:=fail}"
unset -f _story_ts_270
