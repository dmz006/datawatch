#!/usr/bin/env bash
# TS-168 — Stop + restart: memory persists
# tags: surface:docker feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-168"
story_preflight "surface:docker feature:memory" || return 0

_story_ts_168() {
  skip "docker isolation test: requires manual run with docker access"
}

RESULT=fail
_story_ts_168
: "${RESULT:=fail}"
unset -f _story_ts_168
