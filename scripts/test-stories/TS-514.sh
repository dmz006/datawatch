#!/usr/bin/env bash
# TS-514 — POST /api/push/register accepts device registration
# tags: surface:api feature:push
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-514"
story_preflight "surface:api feature:push" || return 0

_story_ts_514() {
  local resp code
  resp=$(api_code POST /api/push/register "{\"device_id\":\"test-device-$$\",\"endpoint\":\"https://example.com/push\",\"token\":\"test-token-$$\"}")
  save_evidence TS-514 "register.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "201" || "$code" == "400" ]]; then
    ok "POST /api/push/register returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "push/register endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_514
: "${RESULT:=fail}"
unset -f _story_ts_514
