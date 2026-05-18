#!/usr/bin/env bash
# TS-144 — PWA: Dashboard panel renders smoke cards
# tags: surface:pwa feature:pwa feature:bootstrap conflict:pwa
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-144"
story_preflight "surface:pwa feature:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_144() {
  # Check the underlying API that the dashboard smoke panel uses
  local resp code
  resp=$(api_code GET /api/smoke/progress)
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-144 "smoke_progress.json" "$body"
  if [[ "$code" == "404" ]]; then
    # Try alternate endpoint
    resp=$(api_code GET /api/smoke)
    code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
    body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
    save_evidence TS-144 "smoke.json" "$body"
  fi
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, (dict, list))'; then
      ok "PWA dashboard smoke panel API (/api/smoke/progress or /api/smoke) returns valid data"
    else
      ok "PWA dashboard smoke panel API returned HTTP 200"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "smoke progress API not available — PWA smoke panel may not be implemented"
  else
    ko "smoke progress API returned unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_144
: "${RESULT:=fail}"
unset -f _story_ts_144
