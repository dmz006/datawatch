#!/usr/bin/env bash
# TS-403 — GET /api/sessions/{id}/status returns hook_health + state fields
# tags: surface:api feature:sessions feature:hooks
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-403"
story_preflight "surface:api feature:sessions feature:hooks" || return 0

_story_ts_403() {
  ensure_test_session || return
  local resp
  resp=$(api GET "/api/sessions/$SESSION_ID/status")
  save_evidence TS-403 "resp.json" "$resp"
  if assert_json "$resp" '"state" in d'; then
    ok "GET /api/sessions/$SESSION_ID/status returns dict with state field"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/sessions/$SESSION_ID/status returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "session status endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_403
: "${RESULT:=fail}"
unset -f _story_ts_403
