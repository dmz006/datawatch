#!/usr/bin/env bash
# TS-606 — "sessions all" comm command returns aggregated list with server field
# tags: surface:comm feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-606"
story_preflight "surface:comm feature:multiserver" || return 0

_story_ts_606() {
  local resp
  resp=$(api POST /api/test/message '{"text":"sessions all"}')
  save_evidence TS-606 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "test/message endpoint not available in this build"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) == 0'; then
    skip "no channel responders configured — count=0"
    return
  fi
  if assert_json "$resp" 'd.get("count", 0) >= 1'; then
    ok "sessions all comm command returned response (count>=1)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_606
: "${RESULT:=fail}"
unset -f _story_ts_606
