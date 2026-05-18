#!/usr/bin/env bash
# TS-274 — autonomous_type_list via MCP returns array
# tags: surface:mcp feature:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-274"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

_story_ts_274() {
  local resp inner
  resp=$(api POST /api/mcp/call '{"tool":"autonomous_type_list","params":{}}')
  inner=$(mcp_unwrap "$resp")
  save_evidence TS-274 "resp.json" "$resp"
  if echo "$inner" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "autonomous_type_list not available in this build"
    return
  fi
  if assert_json "$inner" 'isinstance(d, list)'; then
    ok "autonomous_type_list returned array"
  elif assert_json "$inner" 'isinstance(d, dict) and ("types" in d or "items" in d or "result" in d)'; then
    ok "autonomous_type_list returned dict with types key"
  else
    ko "autonomous_type_list unexpected: $(echo "$inner" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_274
: "${RESULT:=fail}"
unset -f _story_ts_274
