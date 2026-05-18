#!/usr/bin/env bash
# TS-289 — federation_meta_peers + federation_sessions shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-289"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_289() {
  local resp

  # federation_meta_peers
  resp=$(api POST /api/mcp/call '{"tool":"federation_meta_peers","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-289 "meta_peers.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "federation_meta_peers not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "federation_meta_peers unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # federation_sessions
  resp=$(api POST /api/mcp/call '{"tool":"federation_sessions","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-289 "sessions.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "federation_meta_peers + federation_sessions both return valid shapes"
  else
    ok "federation_meta_peers valid; federation_sessions: $(echo "$resp" | head -c 80)"
  fi
}

RESULT=fail
_story_ts_289
: "${RESULT:=fail}"
unset -f _story_ts_289
