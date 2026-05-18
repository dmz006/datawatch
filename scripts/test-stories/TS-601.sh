#!/usr/bin/env bash
# TS-601 — PWA input on remote session proxies through /api/sessions/{peer}/{id}/input
# tags: surface:pwa feature:multiserver
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-601"
story_preflight "surface:pwa feature:multiserver" || return 0

_story_ts_601() {
  skip "PWA remote session proxy input test not yet automated"
}

RESULT=fail
_story_ts_601
: "${RESULT:=fail}"
unset -f _story_ts_601
