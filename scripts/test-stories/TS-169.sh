#!/usr/bin/env bash
# TS-169 — Isolated stats shows separate uptime
# tags: surface:docker feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-169"
story_preflight "surface:docker feature:bootstrap" || return 0

_story_ts_169() {
  skip "docker isolation test: requires manual run with docker access"
}

RESULT=fail
_story_ts_169
: "${RESULT:=fail}"
unset -f _story_ts_169
