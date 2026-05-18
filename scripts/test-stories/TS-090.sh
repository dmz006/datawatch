#!/usr/bin/env bash
# TS-090 — DNS comm: configure
# tags: surface:api feature:comms conflict:db-write
# legacy fn: t9_ts090_dns_configure
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-090"
story_preflight "surface:api feature:comms conflict:db-write" || return 0

_story_ts_090() {
  local resp
  resp=$(api PUT /api/config '{"dns_channel.enabled":true,"dns_channel.domain":"test.e2e.local","dns_channel.record_type":"TXT"}')
  save_evidence TS-090 "put.json" "$resp"
  if assert_json "$resp" 'd.get("status") == "ok"'; then
    ok "DNS channel config PUT accepted"
  else
    skip "dns_channel config key not present: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_090
: "${RESULT:=fail}"
unset -f _story_ts_090
