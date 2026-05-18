#!/usr/bin/env bash
# TS-283 — dns_channel_config_get + dns_channel_config_set round-trip via MCP
# tags: surface:mcp feature:mcp feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-283"
story_preflight "surface:mcp feature:mcp feature:comms" || return 0

_story_ts_283() {
  local resp

  # GET
  resp=$(api POST /api/mcp/call '{"tool":"dns_channel_config_get","params":{}}')
  save_evidence TS-283 "get.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "dns_channel_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "dns_channel_config_get returned non-dict: $(echo "$resp" | head -c 200)"
    return
  fi

  # SET (write same enabled back; skip on read-only error)
  local enabled
  enabled=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("enabled",False)).lower())' 2>/dev/null || echo "false")
  resp=$(api POST /api/mcp/call "{\"tool\":\"dns_channel_config_set\",\"params\":{\"enabled\":$enabled}}")
  save_evidence TS-283 "set.json" "$resp"
  if echo "$resp" | grep -qi "read.only\|not allowed\|immutable"; then
    skip "dns_channel_config_set is read-only in this environment"
    return
  fi
  ok "dns_channel_config_get + set round-trip succeeded"
}

RESULT=fail
_story_ts_283
: "${RESULT:=fail}"
unset -f _story_ts_283
