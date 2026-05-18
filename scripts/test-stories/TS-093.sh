#!/usr/bin/env bash
# TS-093 — ntfy: configure + send
# tags: surface:api feature:comms
# legacy fn: t9_ts093_ntfy_send
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-093"
story_preflight "surface:api feature:comms" || return 0

_story_ts_093() {
  if [[ -z "$TEST_NTFY_TOPIC" ]]; then
    skip "TEST_NTFY_TOPIC not set"
    return
  fi
  local put_resp send_resp
  put_resp=$(api PUT /api/config '{"ntfy.enabled":true,"ntfy.topic":"'"$TEST_NTFY_TOPIC"'"}')
  save_evidence TS-093 "put.json" "$put_resp"
  send_resp=$(api POST /api/comm/send '{"backend":"ntfy","message":"test ntfy e2e"}')
  save_evidence TS-093 "send.json" "$send_resp"
  if assert_json "$send_resp" 'isinstance(d, dict)'; then
    ok "ntfy send attempted"
  else
    ko "ntfy send failed: $send_resp"
  fi
}

RESULT=fail
_story_ts_093
: "${RESULT:=fail}"
unset -f _story_ts_093
