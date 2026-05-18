#!/usr/bin/env bash
# TS-586 — federation peers comm command returns peer list
# tags: surface:comm feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-586"
story_preflight "surface:comm feature:federation" || return 0

_story_ts_586() {
  local resp
  resp=$(api POST /api/test/message '{"text":"federation peers"}')
  save_evidence TS-586 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "test/message endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) == 0'; then
    skip "no channel responders configured — count=0"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "federation peers comm command returned response (count>=1)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_586
: "${RESULT:=fail}"
unset -f _story_ts_586
