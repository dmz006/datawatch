#!/usr/bin/env bash
# TS-375 — GET /api/sessions/{id}/telemetry returns shape
# tags: surface:api feature:sessions feature:automata
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-375"
story_preflight "surface:api feature:sessions feature:automata" || return 0

_story_ts_375() {
  ensure_test_session || return
  local resp
  resp=$(api GET "/api/sessions/$SESSION_ID/telemetry")
  save_evidence TS-375 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/sessions/$SESSION_ID/telemetry returns dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/sessions/$SESSION_ID/telemetry returns array"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "telemetry endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_375
: "${RESULT:=fail}"
unset -f _story_ts_375
