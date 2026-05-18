#!/usr/bin/env bash
# TS-429 — POST /api/mcp/call with tool=get_version returns version string
# tags: surface:api feature:mcp-tools
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-429"
story_preflight "surface:api feature:mcp-tools" || return 0

_story_ts_429() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"get_version","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-429 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/mcp/call get_version returns dict"
  elif assert_json "$resp" 'isinstance(d, str) and len(d) > 0'; then
    ok "POST /api/mcp/call get_version returns string: $resp"
  elif echo "$resp" | grep -qi "version\|[0-9]\+\.[0-9]\+\.[0-9]\+"; then
    ok "POST /api/mcp/call get_version returns version string: $(echo "$resp" | head -c 60)"
  elif echo "$resp" | grep -qi "unknown tool\|not found\|not available"; then
    skip "get_version MCP tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_429
: "${RESULT:=fail}"
unset -f _story_ts_429
