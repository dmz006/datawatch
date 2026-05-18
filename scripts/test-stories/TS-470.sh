#!/usr/bin/env bash
# TS-470 — YAML autonomous.planning_backend accepted by config reload
# tags: surface:locale feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-470"
story_preflight "surface:locale feature:automata" || return 0

_story_ts_470() {
  skip "config is read-only in test container — test YAML parsing manually"
}

RESULT=fail
_story_ts_470
: "${RESULT:=fail}"
unset -f _story_ts_470
