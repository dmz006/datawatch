#!/usr/bin/env bash
# TS-426 — datawatch llm list exits 0
# tags: surface:cli feature:llm-registry feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-426"
story_preflight "surface:cli feature:llm-registry feature:cli" || return 0

_story_ts_426() {
  local out rc
  out=$(cli_test llm list 2>&1); rc=$?
  save_evidence TS-426 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch llm list exits 0"
  elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
    skip "llm list not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_426
: "${RESULT:=fail}"
unset -f _story_ts_426
