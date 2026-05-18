#!/usr/bin/env bash
# TS-538 — datawatch council run --async exits 0
# tags: surface:cli feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-538"
story_preflight "surface:cli feature:council" || return 0

_story_ts_538() {
  local out rc
  out=$(cli_test council run --async --question "1+1=?" 2>&1); rc=$?
  save_evidence TS-538 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch council run --async exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such|--async"; then
    skip "council run --async not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_538
: "${RESULT:=fail}"
unset -f _story_ts_538
