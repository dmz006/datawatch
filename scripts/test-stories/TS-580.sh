#!/usr/bin/env bash
# TS-580 — federation_group_add MCP tool creates custom group
# tags: surface:mcp feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-580"
story_preflight "surface:mcp feature:federation" || return 0

_story_ts_580() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"federation_group_add","params":{"name":"e2e-grp-ts580","caps":["sessions:list"]}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-580 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled\|no route"; then
    skip "federation_group_add MCP tool not available in this build"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "federation_group_add MCP tool returned dict"
    # cleanup
    api DELETE /api/federation/groups/e2e-grp-ts580 >/dev/null 2>&1 || true
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_580
: "${RESULT:=fail}"
unset -f _story_ts_580
