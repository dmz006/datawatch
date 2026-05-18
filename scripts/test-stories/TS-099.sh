#!/usr/bin/env bash
# TS-099 — !mcp comm command
# tags: surface:api feature:comms feature:mcp
# legacy fn: t9_ts099_mcp_command
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-099"
story_preflight "surface:api feature:comms feature:mcp" || return 0

_story_ts_099() {
  local resp
  resp=$(api POST /api/test/message '{"text":"mcp"}')
  save_evidence TS-099 "mcp.json" "$resp"
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "!mcp command returned response"
  else
    ko "!mcp command failed: $resp"
  fi
}

RESULT=fail
_story_ts_099
: "${RESULT:=fail}"
unset -f _story_ts_099
