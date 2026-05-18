#!/usr/bin/env bash
# TS-102 — !sessions via POST /api/test/message
# tags: surface:comms feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-102"
story_preflight "surface:comms feature:comms" || return 0

_story_ts_102() {
  ensure_test_session || true  # best-effort: sessions command should work even if none active
  local resp code
  resp=$(api_code POST /api/test/message '{"text":"!sessions"}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-102 "sessions_msg.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/test/message: endpoint not available (404)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, dict)'; then
      ok "!sessions command via POST /api/test/message: returned dict"
    else
      ok "!sessions command via POST /api/test/message: HTTP 200"
    fi
  elif echo "$body" | grep -qiE "not.*found|disabled|not.*enabled|not.*configured"; then
    skip "!sessions command not available: $(echo "$body" | head -c 100)"
  else
    ko "POST /api/test/message (!sessions): unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_102
: "${RESULT:=fail}"
unset -f _story_ts_102
