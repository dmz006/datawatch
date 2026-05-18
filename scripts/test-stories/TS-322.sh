#!/usr/bin/env bash
# TS-322 — datawatch evals runs exits 0
# tags: surface:cli feature:cli feature:evals
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-322"
story_preflight "surface:cli feature:cli feature:evals" || return 0

_story_ts_322() {
  local out; out=$(cli_test evals runs 2>&1); local rc=$?
  save_evidence TS-322 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "evals runs exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    skip "evals not available: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_322
: "${RESULT:=fail}"
unset -f _story_ts_322
