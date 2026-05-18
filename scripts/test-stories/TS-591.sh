#!/usr/bin/env bash
# TS-591 — Peer token viewer → Federation Peers card is read-only
# tags: surface:pwa feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-591"
story_preflight "surface:pwa feature:federation feature:cbac" || return 0

_story_ts_591() {
  skip "PWA CBAC federation peer card read-only test not yet automated"
}

RESULT=fail
_story_ts_591
: "${RESULT:=fail}"
unset -f _story_ts_591
