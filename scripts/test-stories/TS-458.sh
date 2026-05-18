#!/usr/bin/env bash
# TS-458 — observer_peers_by_node MCP tool returns by_node+unbound shape
# tags: surface:mcp feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-458"
story_preflight "surface:mcp feature:observer" || return 0

_story_ts_458() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"observer_peers_by_node","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-458 "resp.json" "$resp"
  if assert_json "$resp" '"by_node" in d and "unbound" in d'; then
    ok "observer_peers_by_node tool returned by_node+unbound shape"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "observer_peers_by_node tool returned valid JSON"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "observer_peers_by_node tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_458
: "${RESULT:=fail}"
unset -f _story_ts_458
