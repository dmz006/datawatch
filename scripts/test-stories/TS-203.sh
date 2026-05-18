#!/usr/bin/env bash
# TS-203 — 
# tags: 
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-203"

_story_ts_203() {
  skip "push-notifications: requires NTFY topic (set TEST_NTFY_TOPIC)"
}

RESULT=fail
_story_ts_203
: "${RESULT:=fail}"
unset -f _story_ts_203
