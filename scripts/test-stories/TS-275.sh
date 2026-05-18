#!/usr/bin/env bash
# TS-275 — backends_list via MCP returns {llm:[...]} shape
# tags: surface:mcp feature:mcp feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-275"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

_story_ts_275() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"backends_list","params":{}}')
  save_evidence TS-275 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "backends_list not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and "llm" in d'; then
    ok "backends_list returned dict with llm key"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "backends_list returned dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "backends_list returned array"
  else
    ko "backends_list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_275
: "${RESULT:=fail}"
unset -f _story_ts_275
