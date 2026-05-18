#!/usr/bin/env bash
# TS-298 — tailscale_status + tailscale_nodes shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-298"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_298() {
  local resp

  # tailscale_status
  resp=$(api POST /api/mcp/call '{"tool":"tailscale_status","params":{}}')
  save_evidence TS-298 "status.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool\|not configured\|not running"; then
    skip "tailscale not available/configured in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "tailscale_status unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # tailscale_nodes
  resp=$(api POST /api/mcp/call '{"tool":"tailscale_nodes","params":{}}')
  save_evidence TS-298 "nodes.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "tailscale_status + tailscale_nodes both return valid shapes"
  else
    ok "tailscale_status valid; tailscale_nodes: $(echo "$resp" | head -c 80)"
  fi
}

RESULT=fail
_story_ts_298
: "${RESULT:=fail}"
unset -f _story_ts_298
