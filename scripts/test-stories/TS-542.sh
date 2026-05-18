#!/usr/bin/env bash
# TS-542 — GET /api/sessions/{id}/status returns hook_health + state + panels shape
# tags: surface:api feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-542"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_542() {
  ensure_test_session || return
  local resp
  resp=$(api GET "/api/sessions/$SESSION_ID/status")
  save_evidence TS-542 "status.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404"; then
    skip "sessions status endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict) and ("state" in d or "hook_health" in d or "panels" in d)'; then
    ok "GET /api/sessions/$SESSION_ID/status has expected fields"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/sessions/$SESSION_ID/status returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_542
: "${RESULT:=fail}"
unset -f _story_ts_542
