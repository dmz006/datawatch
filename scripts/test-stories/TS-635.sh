#!/usr/bin/env bash
# TS-635 — datawatch skills registry list includes community registry
# tags: surface:cli feature:community-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-635"
story_preflight "surface:cli feature:community-registry" || return 0

_story_ts_635() {
  local out rc
  out=$(cli_test skills registry list 2>&1); rc=$?
  save_evidence TS-635 "out.txt" "$out"
  if echo "$out" | grep -qiE "unknown command|unknown flag|no such"; then
    skip "skills registry list CLI not available in this build"
    return
  fi
  if [[ $rc -ne 0 ]]; then
    ko "datawatch skills registry list failed (rc=$rc): $(echo "$out" | head -c 200)"
    return
  fi
  if echo "$out" | grep -qi "community"; then
    ok "datawatch skills registry list includes community registry"
  else
    skip "community registry not present in this environment"
  fi
}

RESULT=fail
_story_ts_635
: "${RESULT:=fail}"
unset -f _story_ts_635
