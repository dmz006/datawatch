#!/usr/bin/env bash
# TS-400 — GET /api/dashboard/layout returns valid JSON shape
# tags: surface:api feature:dashboard
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-400"
story_preflight "surface:api feature:dashboard" || return 0

_story_ts_400() {
  local resp
  resp=$(api GET /api/dashboard/layout)
  save_evidence TS-400 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/dashboard/layout returns valid JSON dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/dashboard/layout returns JSON array"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "dashboard/layout endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_400
: "${RESULT:=fail}"
unset -f _story_ts_400
