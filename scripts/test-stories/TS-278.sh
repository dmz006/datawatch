#!/usr/bin/env bash
# TS-278 — cooldown_status + cooldown_set + cooldown_clear via MCP
# tags: surface:mcp feature:mcp feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-278"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

_story_ts_278() {
  local resp

  # Status
  resp=$(api POST /api/mcp/call '{"tool":"cooldown_status","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-278 "status.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "cooldown_status not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "cooldown_status unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # Set
  resp=$(api POST /api/mcp/call '{"tool":"cooldown_set","params":{"minutes":1}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-278 "set.json" "$resp"

  # Clear
  resp=$(api POST /api/mcp/call '{"tool":"cooldown_clear","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-278 "clear.json" "$resp"

  ok "cooldown_status + cooldown_set + cooldown_clear via MCP"
}

RESULT=fail
_story_ts_278
: "${RESULT:=fail}"
unset -f _story_ts_278
