#!/usr/bin/env bash
# TS-600 — PWA Sessions All mode shows cards from federation peers with server badge
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-600"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_600() {
  skip "PWA multiserver sessions All mode test not yet automated"
}

RESULT=fail
_story_ts_600
: "${RESULT:=fail}"
unset -f _story_ts_600
