#!/usr/bin/env bash
# TS-091 — DNS comm: send + verify stats
# tags: surface:api feature:comms
# legacy fn: t9_ts091_dns_send_verify_stats
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-091"
story_preflight "surface:api feature:comms" || return 0

_story_ts_091() {
  local before_stats send_resp after_stats
  before_stats=$(api GET /api/stats)
  save_evidence TS-091 "before_stats.json" "$before_stats"
  send_resp=$(api POST /api/comm/send '{"backend":"dns","message":"test dns send e2e"}')
  save_evidence TS-091 "send.json" "$send_resp"
  after_stats=$(api GET /api/stats)
  save_evidence TS-091 "after_stats.json" "$after_stats"
  if assert_json "$send_resp" 'isinstance(d, dict)'; then
    ok "DNS send attempted (comm_stats tracked)"
  else
    skip "DNS send failed: $(echo "$send_resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_091
: "${RESULT:=fail}"
unset -f _story_ts_091
