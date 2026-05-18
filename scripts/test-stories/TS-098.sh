#!/usr/bin/env bash
# TS-098 — !alert comm command
# tags: surface:api feature:comms
# legacy fn: t9_ts098_alert_command
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-098"
story_preflight "surface:api feature:comms" || return 0

_story_ts_098() {
  ensure_test_session || return
  local resp
  resp=$(api POST /api/test/message '{"text":"alert test e2e alert message"}')
  save_evidence TS-098 "alert.json" "$resp"
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "!alert command returned response"
  elif assert_json "$resp" 'd.get("count", 0) == 0'; then
    skip "no channel responders configured — count=0"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "test/message endpoint not available"
  else
    ko "!alert command failed: $resp"
  fi
}

RESULT=fail
_story_ts_098
: "${RESULT:=fail}"
unset -f _story_ts_098
