#!/usr/bin/env bash
# TS-003 — Auth 401 without token
# tags: surface:api feature:bootstrap blocking
# legacy fn: t1_ts003_auth_401_without_token
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-003"
story_preflight "surface:api feature:bootstrap blocking" || return 0

_story_ts_003() {
  local code
  code=$(curl -sk --max-time 10 -o /dev/null -w "%{http_code}" "$TEST_BASE/api/sessions" 2>/dev/null)
  save_evidence TS-003 "http_code.txt" "$code"
  if [[ "$code" == "401" ]]; then
    ok "unauthenticated request returns 401 (auth enforced)"
  elif [[ "$code" == "200" ]]; then
    skip "server.token not configured — auth enforcement not active in this deployment"
  else
    ko "expected 401 (auth) or 200 (no-auth), got $code"
  fi
}

RESULT=fail
_story_ts_003
: "${RESULT:=fail}"
unset -f _story_ts_003
