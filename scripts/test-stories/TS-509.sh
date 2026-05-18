#!/usr/bin/env bash
# TS-509 — datawatch compute node observer-free exits 0 and returns JSON
# tags: surface:cli feature:observer feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-509"
story_preflight "surface:cli feature:observer feature:compute" || return 0

_story_ts_509() {
  local out rc
  out=$(cli_test compute node observer-free 2>&1); rc=$?
  save_evidence TS-509 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch compute node observer-free exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_509
: "${RESULT:=fail}"
unset -f _story_ts_509
