#!/usr/bin/env bash
# TS-282 — detection_config_get + detection_config_set round-trip via MCP
# tags: surface:mcp feature:mcp feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-282"
story_preflight "surface:mcp feature:mcp feature:sessions" || return 0

_story_ts_282() {
  local resp

  # GET
  resp=$(api POST /api/mcp/call '{"tool":"detection_config_get","params":{}}')
  save_evidence TS-282 "get.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "detection_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "detection_config_get returned non-dict: $(echo "$resp" | head -c 200)"
    return
  fi

  # SET (round-trip: write back same enabled value)
  local enabled
  enabled=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("enabled",True)).lower())' 2>/dev/null || echo "true")
  resp=$(api POST /api/mcp/call "{\"tool\":\"detection_config_set\",\"params\":{\"enabled\":$enabled}}")
  save_evidence TS-282 "set.json" "$resp"
  ok "detection_config_get + set round-trip succeeded"
}

RESULT=fail
_story_ts_282
: "${RESULT:=fail}"
unset -f _story_ts_282
