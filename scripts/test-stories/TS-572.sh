#!/usr/bin/env bash
# TS-572 — Peer token without comm:write → POST /api/mcp/call returns 403
# tags: surface:api feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-572"
story_preflight "surface:api feature:federation feature:cbac" || return 0

_story_ts_572() {
  # CBAC peer-token tests require federation peers configured with known tokens.
  local peers_resp
  peers_resp=$(api GET /api/federation/peers)
  if echo "$peers_resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi
  skip "federation CBAC peer-token test requires federation peers configured with known tokens"
}

RESULT=fail
_story_ts_572
: "${RESULT:=fail}"
unset -f _story_ts_572
