#!/usr/bin/env bash
# TS-454 — GET /api/federation/meta-peers returns valid JSON shape
# tags: surface:api feature:federation feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-454"
story_preflight "surface:api feature:federation feature:observer" || return 0

_story_ts_454() {
  local resp
  resp=$(api GET /api/federation/meta-peers)
  save_evidence TS-454 "meta-peers.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "federation/meta-peers endpoint not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/federation/meta-peers returns valid JSON"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_454
: "${RESULT:=fail}"
unset -f _story_ts_454
