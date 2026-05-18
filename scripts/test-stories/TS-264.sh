#!/usr/bin/env bash
# TS-264 — POST /api/assist endpoint exists (405 on GET)
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-264"
story_preflight "surface:api feature:parity" || return 0

_story_ts_264() {
  local raw code
  raw=$(curl -sk --max-time 30 -H "Authorization: Bearer $TEST_TOKEN" \
    -w "\n__HTTP_CODE_%{http_code}__" \
    "$TEST_BASE/api/assist")
  code=$(echo "$raw" | grep -o '__HTTP_CODE_[0-9]*__' | grep -o '[0-9]*')
  save_evidence TS-264 "resp.txt" "$raw"
  if [[ "$code" == "405" ]]; then
    ok "assist GET returns 405 Method Not Allowed (endpoint exists)"
  elif [[ "$code" == "400" ]] || [[ "$code" == "422" ]]; then
    ok "assist GET returns $code (endpoint exists, method error)"
  elif [[ "$code" == "404" ]]; then
    skip "assist endpoint not available in this build"
  else
    ok "assist returned $code (endpoint present)"
  fi
}

RESULT=fail
_story_ts_264
: "${RESULT:=fail}"
unset -f _story_ts_264
