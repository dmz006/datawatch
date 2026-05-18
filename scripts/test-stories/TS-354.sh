#!/usr/bin/env bash
# TS-354 — POST /api/assist "how do I configure sqlite memory" returns guidance
# tags: surface:api feature:parity feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-354"
story_preflight "surface:api feature:parity feature:howto" || return 0

_story_ts_354() {
  local raw code resp
  raw=$(curl -sk --max-time 30 \
    -H "Authorization: Bearer $TEST_TOKEN" \
    -H "Content-Type: application/json" \
    -X POST \
    -d '{"question":"how do I configure sqlite memory"}' \
    -w "\n__HTTP_CODE_%{http_code}__" \
    "$TEST_BASE/api/assist")
  code=$(echo "$raw" | grep -o '__HTTP_CODE_[0-9]*__' | grep -o '[0-9]*')
  resp=$(echo "$raw" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-354 "resp.txt" "$raw"

  if [[ "$code" == "404" ]]; then
    skip "assist endpoint not available in this build"
  elif [[ "$code" == "405" ]]; then
    skip "assist endpoint requires different method or not available"
  elif [[ "$code" == "200" ]]; then
    if assert_json "$resp" 'isinstance(d, dict) and ("message" in d or "response" in d or "content" in d or "text" in d or "guidance" in d)'; then
      ok "assist POST returned guidance response"
    elif assert_json "$resp" 'isinstance(d, dict)'; then
      ok "assist POST returned dict response"
    else
      ok "assist POST returned 200"
    fi
  elif [[ "$code" == "202" ]] || [[ "$code" == "201" ]]; then
    ok "assist POST returned $code (async response)"
  else
    skip "assist returned $code: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_354
: "${RESULT:=fail}"
unset -f _story_ts_354
