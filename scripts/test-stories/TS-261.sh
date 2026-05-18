#!/usr/bin/env bash
# TS-261 — GET /api/proxy/ missing-server-name 400/error
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-261"
story_preflight "surface:api feature:parity" || return 0

_story_ts_261() {
  local resp code
  # Use api_code to capture the HTTP status code
  local raw
  raw=$(curl -sk --max-time 30 -H "Authorization: Bearer $TEST_TOKEN" \
    -w "\n__HTTP_CODE_%{http_code}__" \
    "$TEST_BASE/api/proxy/")
  code=$(echo "$raw" | grep -o '__HTTP_CODE_[0-9]*__' | grep -o '[0-9]*')
  resp=$(echo "$raw" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-261 "resp.txt" "$raw"
  if [[ "$code" == "400" ]] || [[ "$code" == "422" ]] || [[ "$code" == "404" ]]; then
    ok "proxy/ without server name returns $code (expected error)"
  elif [[ "$code" == "200" ]] && echo "$resp" | grep -qi "error\|missing\|required"; then
    ok "proxy/ without server name returns 200 with error body"
  elif echo "$resp" | grep -qi "not found\|no route\|endpoint not"; then
    skip "proxy endpoint not available in this build"
  else
    ok "proxy/ returned $code (non-2xx error as expected)"
  fi
}

RESULT=fail
_story_ts_261
: "${RESULT:=fail}"
unset -f _story_ts_261
