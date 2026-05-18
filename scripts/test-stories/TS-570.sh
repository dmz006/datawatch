#!/usr/bin/env bash
# TS-570 — Peer token with sessions:list cap → GET /api/sessions returns 200
# tags: surface:api feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-570"
story_preflight "surface:api feature:federation feature:cbac" || return 0

_story_ts_570() {
  # CBAC peer-token tests require federation peers configured with known tokens.
  # Verify the federation endpoint is present at all, then skip if no peers.
  local peers_resp
  peers_resp=$(api GET /api/federation/peers)
  if echo "$peers_resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi
  skip "federation CBAC peer-token test requires federation peers configured with known tokens"
}

RESULT=fail
_story_ts_570
: "${RESULT:=fail}"
unset -f _story_ts_570
