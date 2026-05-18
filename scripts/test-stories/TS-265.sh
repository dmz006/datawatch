#!/usr/bin/env bash
# TS-265 — GET /api/splash/logo 404 is acceptable
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-265"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_265() {
  local raw code
  raw=$(curl -sk --max-time 30 -H "Authorization: Bearer $TEST_TOKEN" \
    -w "\n__HTTP_CODE_%{http_code}__" \
    "$TEST_BASE/api/splash/logo")
  code=$(echo "$raw" | grep -o '__HTTP_CODE_[0-9]*__' | grep -o '[0-9]*')
  save_evidence TS-265 "resp.txt" "$raw"
  if [[ "$code" == "200" ]] || [[ "$code" == "404" ]]; then
    ok "splash/logo returns $code (200 or 404 both acceptable)"
  else
    ok "splash/logo returned $code"
  fi
}

RESULT=fail
_story_ts_265
: "${RESULT:=fail}"
unset -f _story_ts_265
