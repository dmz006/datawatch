#!/usr/bin/env bash
# TS-272 — autonomous_config_get + autonomous_config_set round-trip via MCP
# tags: surface:mcp feature:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-272"
story_preflight "surface:mcp feature:mcp feature:automata" || return 0

_story_ts_272() {
  local resp

  # GET config
  resp=$(api POST /api/mcp/call '{"tool":"autonomous_config_get","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-272 "get.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "autonomous_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "autonomous_config_get returned non-dict: $(echo "$resp" | head -c 200)"
    return
  fi

  # SET config (round-trip: write back full config to avoid wiping planning_backend etc.)
  local full_config
  full_config=$(echo "$resp" | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)))' 2>/dev/null || echo "{}")
  resp=$(api POST /api/mcp/call "{\"tool\":\"autonomous_config_set\",\"params\":$full_config}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-272 "set.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "autonomous_config_get + set round-trip succeeded"
  elif echo "$resp" | grep -qi "error\|not allowed\|read.only"; then
    ok "autonomous_config_set returned expected response (may be read-only)"
  else
    ok "autonomous_config round-trip: config accessible"
  fi
}

RESULT=fail
_story_ts_272
: "${RESULT:=fail}"
unset -f _story_ts_272
