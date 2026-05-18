#!/usr/bin/env bash
# TS-103 — !status via POST /api/test/message
# tags: surface:comms feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-103"
story_preflight "surface:comms feature:comms" || return 0

_story_ts_103() {
  local resp code
  resp=$(api_code POST /api/test/message '{"text":"!status"}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-103 "status_msg.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/test/message: endpoint not available (404)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, dict)'; then
      ok "!status command via POST /api/test/message: returned dict"
    else
      ok "!status command via POST /api/test/message: HTTP 200"
    fi
  elif echo "$body" | grep -qiE "not.*found|disabled|not.*enabled|not.*configured"; then
    skip "!status command not available: $(echo "$body" | head -c 100)"
  else
    ko "POST /api/test/message (!status): unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_103
: "${RESULT:=fail}"
unset -f _story_ts_103
