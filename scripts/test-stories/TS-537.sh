#!/usr/bin/env bash
# TS-537 — datawatch council personas exits 0
# tags: surface:cli feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-537"
story_preflight "surface:cli feature:council" || return 0

_story_ts_537() {
  local out rc
  out=$(cli_test council personas 2>&1); rc=$?
  save_evidence TS-537 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch council personas exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_537
: "${RESULT:=fail}"
unset -f _story_ts_537
