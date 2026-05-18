#!/usr/bin/env bash
# TS-557 — release-smoke.sh exits 0
# tags: surface:meta
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-557"
story_preflight "surface:meta" || return 0

_story_ts_557() {
  skip "release smoke: run manually with scripts/release-smoke.sh"
}

RESULT=fail
_story_ts_557
: "${RESULT:=fail}"
unset -f _story_ts_557
