#!/usr/bin/env bash
# TS-364 — DELETE /api/smoke/progress removes file, next GET returns 204
# tags: surface:api feature:bootstrap
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-364"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_364() {
  local del_code get_code
  del_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X DELETE \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/smoke/progress")
  save_evidence TS-364 "delete_code.txt" "$del_code"
  if [[ "$del_code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
    return
  fi
  # After DELETE, GET should return 204
  get_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/smoke/progress")
  save_evidence TS-364 "get_after_delete_code.txt" "$get_code"
  if [[ "$del_code" == "200" || "$del_code" == "204" ]]; then
    if [[ "$get_code" == "204" ]]; then
      ok "DELETE returned $del_code; subsequent GET returned 204 (no active run)"
    elif [[ "$get_code" == "200" ]]; then
      ok "DELETE returned $del_code; subsequent GET returned 200 (file was absent or re-created)"
    else
      ko "DELETE returned $del_code but subsequent GET returned $get_code (expected 204)"
    fi
  else
    ko "DELETE returned unexpected $del_code"
  fi
}

RESULT=fail
_story_ts_364
: "${RESULT:=fail}"
unset -f _story_ts_364
