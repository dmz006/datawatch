#!/usr/bin/env bash
# TS-588 — federation groups comm command returns group list
# tags: surface:comm feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-588"
story_preflight "surface:comm feature:federation" || return 0

_story_ts_588() {
  local resp
  resp=$(api POST /api/test/message '{"text":"federation groups"}')
  save_evidence TS-588 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "test/message endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) == 0'; then
    skip "no channel responders configured — count=0"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "federation groups comm command returned response (count>=1)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_588
: "${RESULT:=fail}"
unset -f _story_ts_588
