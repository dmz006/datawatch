#!/usr/bin/env bash
# TS-629 — datawatch alert-rules list exits 0
# tags: surface:cli feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-629"
story_preflight "surface:cli feature:alert-rules" || return 0

_story_ts_629() {
  local out rc
  out=$(cli_test alert-rules list 2>&1); rc=$?
  save_evidence TS-629 "out.txt" "$out"
  if echo "$out" | grep -qiE "unknown command|unknown flag|no such|help.*alert"; then
    skip "alert-rules list CLI not available in this build"
    return
  fi
  if [[ $rc -eq 0 ]]; then
    ok "datawatch alert-rules list exits 0"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_629
: "${RESULT:=fail}"
unset -f _story_ts_629
