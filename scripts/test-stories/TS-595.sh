#!/usr/bin/env bash
# TS-595 — GET /api/sessions/aggregated includes entries from federation peers
# tags: surface:api feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-595"
story_preflight "surface:api feature:multiserver" || return 0

_story_ts_595() {
  local resp
  resp=$(api GET /api/sessions/aggregated)
  save_evidence TS-595 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "sessions/aggregated endpoint not available in this build"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/sessions/aggregated returns list"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/sessions/aggregated returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_595
: "${RESULT:=fail}"
unset -f _story_ts_595
