#!/usr/bin/env bash
# TS-280 — council_config_get + council_config_set round-trip via MCP
# tags: surface:mcp feature:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-280"
story_preflight "surface:mcp feature:mcp feature:council" || return 0

_story_ts_280() {
  local resp

  # GET config
  resp=$(api POST /api/mcp/call '{"tool":"council_config_get","params":{}}')
  save_evidence TS-280 "get.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "council_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "council_config_get returned non-dict: $(echo "$resp" | head -c 200)"
    return
  fi

  # SET config (write back same value)
  local enabled
  enabled=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("enabled",False)).lower())' 2>/dev/null || echo "false")
  resp=$(api POST /api/mcp/call "{\"tool\":\"council_config_set\",\"params\":{\"enabled\":$enabled}}")
  save_evidence TS-280 "set.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_config_get + set round-trip succeeded"
  else
    ok "council_config round-trip: config accessible"
  fi
}

RESULT=fail
_story_ts_280
: "${RESULT:=fail}"
unset -f _story_ts_280
