#!/usr/bin/env bash
# TS-590 — Add peer modal in PWA creates peer and refreshes list
# tags: surface:pwa feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-590"
story_preflight "surface:pwa feature:federation" || return 0

_story_ts_590() {
  skip "PWA federation add-peer modal test not yet automated"
}

RESULT=fail
_story_ts_590
: "${RESULT:=fail}"
unset -f _story_ts_590
