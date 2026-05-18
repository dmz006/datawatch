#!/usr/bin/env bash
# TS-004 — Auth 200 with correct token
# tags: surface:api feature:bootstrap blocking
# legacy fn: t1_ts004_auth_200_with_token
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-004"
story_preflight "surface:api feature:bootstrap blocking" || return 0

_story_ts_004() {
  local resp code
  resp=$(api_code GET /api/sessions)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body; body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-004 "sessions.json" "$body"
  save_evidence TS-004 "http_code.txt" "$code"
  if [[ "$code" == "200" ]]; then
    ok "authenticated request returns 200"
  else
    ko "expected 200 with token, got $code"
  fi
}

RESULT=fail
_story_ts_004
: "${RESULT:=fail}"
unset -f _story_ts_004
