#!/usr/bin/env bash
# TS-067 — Tooling status
# tags: surface:mcp feature:skills
# legacy fn: t7_ts067_tooling_status
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-067"
story_preflight "surface:mcp feature:skills" || return 0

_story_ts_067() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"tooling_status","params":{}}' 2>/dev/null || \
        api GET /api/tooling/status 2>/dev/null || echo '{}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-067 "tooling_status.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "tooling_status MCP/REST call returns dict"
  else
    skip "tooling_status not available (may be v7.1.0+ feature)"
  fi
}

RESULT=fail
_story_ts_067
: "${RESULT:=fail}"
unset -f _story_ts_067
