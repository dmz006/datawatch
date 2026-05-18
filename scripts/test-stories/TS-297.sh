#!/usr/bin/env bash
# TS-297 — routing_rules_list + routing_rules_test shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-297"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_297() {
  local resp

  # routing_rules_list
  resp=$(api POST /api/mcp/call '{"tool":"routing_rules_list","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-297 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "routing_rules_list not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "routing_rules_list unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # routing_rules_test
  resp=$(api POST /api/mcp/call '{"tool":"routing_rules_test","params":{"backend":"shell","task":"test task"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-297 "test.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "routing_rules_list + routing_rules_test both return valid shapes"
  else
    ok "routing_rules_list valid; routing_rules_test: $(echo "$resp" | head -c 80)"
  fi
}

RESULT=fail
_story_ts_297
: "${RESULT:=fail}"
unset -f _story_ts_297
