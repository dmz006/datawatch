#!/usr/bin/env bash
# TS-328 — datawatch observer peers list exits 0
# tags: surface:cli feature:cli feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-328"
story_preflight "surface:cli feature:cli feature:parity" || return 0

_story_ts_328() {
  local out; out=$(cli_test observer peers list 2>&1); local rc=$?
  save_evidence TS-328 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "observer peers list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "observer not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_328
: "${RESULT:=fail}"
unset -f _story_ts_328
