#!/usr/bin/env bash
# TS-508 — datawatch compute node list exits 0
# tags: surface:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-508"
story_preflight "surface:cli feature:compute" || return 0

_story_ts_508() {
  local out rc
  out=$(cli_test compute node list 2>&1); rc=$?
  save_evidence TS-508 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch compute node list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_508
: "${RESULT:=fail}"
unset -f _story_ts_508
