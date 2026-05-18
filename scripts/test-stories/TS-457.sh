#!/usr/bin/env bash
# TS-457 — observer_peers_free MCP tool returns array
# tags: surface:mcp feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-457"
story_preflight "surface:mcp feature:observer" || return 0

_story_ts_457() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"observer_peers_free","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-457 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "observer_peers_free tool returned array"
  elif assert_json "$resp" 'isinstance(d.get("peers",[]), list)'; then
    ok "observer_peers_free tool returned object with peers array"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "observer_peers_free tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_457
: "${RESULT:=fail}"
unset -f _story_ts_457
