#!/usr/bin/env bash
# TS-589 — Observer tab shows Federation Peers card
# tags: surface:pwa feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-589"
story_preflight "surface:pwa feature:federation" || return 0

_story_ts_589() {
  # Confirm federation API is reachable, then skip PWA visual test
  local resp
  resp=$(api GET /api/federation/peers)
  save_evidence TS-589 "api.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "federation/peers endpoint not available in this build"
    return
  fi
  skip "PWA federation observer tab test not yet automated"
}

RESULT=fail
_story_ts_589
: "${RESULT:=fail}"
unset -f _story_ts_589
