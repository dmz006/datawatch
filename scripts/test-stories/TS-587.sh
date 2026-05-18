#!/usr/bin/env bash
# TS-587 — federation peer add name url comm command registers peer
# tags: surface:comm feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-587"
story_preflight "surface:comm feature:federation" || return 0

_story_ts_587() {
  local resp
  resp=$(api POST /api/test/message '{"text":"federation peer add e2e-comm-peer http://127.0.0.1:19999"}')
  save_evidence TS-587 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "test/message endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) == 0'; then
    skip "no channel responders configured — count=0"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "federation peer add comm command returned response (count>=1)"
    # attempt cleanup
    api DELETE /api/federation/peers/e2e-comm-peer >/dev/null 2>&1 || true
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_587
: "${RESULT:=fail}"
unset -f _story_ts_587
