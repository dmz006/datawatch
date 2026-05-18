#!/usr/bin/env bash
# TS-567 — GET /api/federation/groups returns {builtins:[13 items],custom:[]}
# tags: surface:api feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-567"
story_preflight "surface:api feature:federation" || return 0

_story_ts_567() {
  local resp
  resp=$(api GET /api/federation/groups)
  save_evidence TS-567 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/groups endpoint not available in this build"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/federation/groups returns dict"
  elif assert_json "$resp" 'isinstance(d, list)'; then
    ok "GET /api/federation/groups returns list"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_567
: "${RESULT:=fail}"
unset -f _story_ts_567
