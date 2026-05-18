#!/usr/bin/env bash
# TS-599 — PWA server picker shows federation peers
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-599"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_599() {
  skip "PWA multiserver server picker UI test not yet automated"
}

RESULT=fail
_story_ts_599
: "${RESULT:=fail}"
unset -f _story_ts_599
