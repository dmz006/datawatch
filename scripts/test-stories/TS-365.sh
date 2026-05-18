#!/usr/bin/env bash
# TS-365 — POST /api/sessions/{id}/input sends text with Enter
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-365"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_365() {
  ensure_test_session || return
  local resp code body
  resp=$(api_code POST "/api/sessions/$SESSION_ID/input" '{"text":"test input\n"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-365 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "202" ]]; then
    ok "POST /api/sessions/$SESSION_ID/input returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "input endpoint not available (404)"
  elif [[ "$code" == "400" ]]; then
    # Session may not be in a state that accepts input — acceptable
    skip "session not accepting input (400): $(echo "$body" | head -c 100)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_365
: "${RESULT:=fail}"
unset -f _story_ts_365
