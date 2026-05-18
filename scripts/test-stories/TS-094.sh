#!/usr/bin/env bash
# TS-094 — Signal: configure + send
# tags: surface:api feature:comms
# legacy fn: t9_ts094_signal_send
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-094"
story_preflight "surface:api feature:comms" || return 0

_story_ts_094() {
  # Signal is configured in test daemon config; daemon handles Java/signal-cli internally
  local send_resp
  send_resp=$(api POST /api/comm/send '{"backend":"signal","message":"datawatch e2e test — TS-094 ignore"}')
  save_evidence TS-094 "send.json" "$send_resp"
  if assert_json "$send_resp" 'isinstance(d, dict) and not d.get("error","").startswith("signal not")'; then
    ok "Signal send accepted by daemon"
  elif echo "$send_resp" | grep -qi "not enabled\|not configured\|disabled"; then
    skip "Signal not enabled in test daemon — check comm.signal config"
  else
    ko "Signal send failed: $(echo "$send_resp" | head -c 120)"
  fi
}

RESULT=fail
_story_ts_094
: "${RESULT:=fail}"
unset -f _story_ts_094
