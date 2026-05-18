#!/usr/bin/env bash
# TS-468 — datawatch autonomous prd-decompose exits 0 (or skip if not configured)
# tags: surface:cli feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-468"
story_preflight "surface:cli feature:automata" || return 0

_story_ts_468() {
  local out rc
  out=$(cli_test autonomous prd-decompose --help 2>&1); rc=$?
  save_evidence TS-468 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch autonomous prd-decompose --help exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "prd-decompose not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_468
: "${RESULT:=fail}"
unset -f _story_ts_468
