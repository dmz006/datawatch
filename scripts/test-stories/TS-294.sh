#!/usr/bin/env bash
# TS-294 — observer_config_get + observer_peers_list + observer_stats via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-294"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_294() {
  local resp

  # observer_config_get
  resp=$(api POST /api/mcp/call '{"tool":"observer_config_get","params":{}}')
  save_evidence TS-294 "config.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "observer_config_get not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, dict)'; then
    ko "observer_config_get unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # observer_peers_list
  resp=$(api POST /api/mcp/call '{"tool":"observer_peers_list","params":{}}')
  save_evidence TS-294 "peers.json" "$resp"

  # observer_stats
  resp=$(api POST /api/mcp/call '{"tool":"observer_stats","params":{}}')
  save_evidence TS-294 "stats.json" "$resp"

  ok "observer_config_get + observer_peers_list + observer_stats all returned"
}

RESULT=fail
_story_ts_294
: "${RESULT:=fail}"
unset -f _story_ts_294
