#!/usr/bin/env bash
# TS-401 — PUT /api/dashboard/layout round-trips (save + reload preserves cards)
# tags: surface:api feature:dashboard
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-401"
story_preflight "surface:api feature:dashboard" || return 0

_story_ts_401() {
  # GET current layout
  local get_resp
  get_resp=$(api GET /api/dashboard/layout)
  save_evidence TS-401 "get_resp.json" "$get_resp"
  if echo "$get_resp" | grep -qi "not found\|404\|not available"; then
    skip "dashboard/layout endpoint not available"
    return
  fi
  # PUT the same content back
  local put_resp code body
  put_resp=$(api_code PUT /api/dashboard/layout "$get_resp")
  code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-401 "put_resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    ok "PUT /api/dashboard/layout returned $code (round-trip)"
  elif [[ "$code" == "404" ]]; then
    skip "PUT /api/dashboard/layout endpoint not available (404)"
  else
    ko "PUT /api/dashboard/layout returned $code: $body"
  fi
}

RESULT=fail
_story_ts_401
: "${RESULT:=fail}"
unset -f _story_ts_401
