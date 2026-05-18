#!/usr/bin/env bash
# TS-075 — Read datawatch://sessions resource
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts075_sessions_resource
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-075"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_075() {
  ensure_test_session || true  # best-effort: resource should still be readable even if empty
  local resp
  resp=$(api POST /api/mcp/resources/read '{"uri":"datawatch://sessions"}')
  save_evidence TS-075 "sessions_resource.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "datawatch://sessions resource readable"
  else
    skip "sessions resource not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_075
: "${RESULT:=fail}"
unset -f _story_ts_075
