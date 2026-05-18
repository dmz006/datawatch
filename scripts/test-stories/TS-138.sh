#!/usr/bin/env bash
# TS-138 — MCP panel tools list
# tags: surface:pwa feature:mcp conflict:pwa
# legacy fn: t11_ts138_mcp_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-138"
story_preflight "surface:pwa feature:mcp conflict:pwa" || return 0

_story_ts_138() {
  local resp
  resp=$(api GET /api/mcp/docs)
  save_evidence TS-138 "mcp_docs.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "MCP docs endpoint works"
  else
    ko "MCP docs endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_138
: "${RESULT:=fail}"
unset -f _story_ts_138
