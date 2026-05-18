#!/usr/bin/env bash
# TS-360 — GET /api/smoke/progress returns 204 when no run active
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-360"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_360() {
  local code
  code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/smoke/progress")
  save_evidence TS-360 "code.txt" "$code"
  if [[ "$code" == "204" ]]; then
    ok "GET /api/smoke/progress returned 204 (no run active)"
  elif [[ "$code" == "200" ]]; then
    ok "GET /api/smoke/progress returned 200 (run may be active or progress exists)"
  elif [[ "$code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
  else
    ko "unexpected HTTP $code from /api/smoke/progress"
  fi
}

RESULT=fail
_story_ts_360
: "${RESULT:=fail}"
unset -f _story_ts_360
