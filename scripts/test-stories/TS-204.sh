#!/usr/bin/env bash
# TS-204 — 
# tags: 
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-204"

_story_ts_204() {
  skip "pipeline-chaining: requires configured pipeline (run manually)"
}

RESULT=fail
_story_ts_204
: "${RESULT:=fail}"
unset -f _story_ts_204
