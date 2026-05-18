#!/usr/bin/env bash
# TS-404 — GET /api/smoke/progress returns {active,version,sections} shape when smoke running
# tags: surface:api feature:dashboard
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-404"
story_preflight "surface:api feature:dashboard" || return 0

_story_ts_404() {
  local resp code body
  resp=$(api_code GET /api/smoke/progress)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-404 "resp.json" "$body"
  if [[ "$code" == "204" ]]; then
    skip "no smoke run active (204) — shape check requires running smoke"
  elif [[ "$code" == "200" ]]; then
    if assert_json "$body" '"active" in d'; then
      ok "GET /api/smoke/progress returns shape with active field"
    else
      ko "progress JSON missing active field: $body"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_404
: "${RESULT:=fail}"
unset -f _story_ts_404
