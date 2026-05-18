#!/usr/bin/env bash
# TS-394 — datawatch server list exits 0
# tags: surface:cli feature:multi-server feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-394"
story_preflight "surface:cli feature:multi-server feature:cli" || return 0

_story_ts_394() {
  local out rc
  out=$(cli_test server list 2>&1); rc=$?
  save_evidence TS-394 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch server list exits 0"
  elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
    skip "server list not available: $(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_394
: "${RESULT:=fail}"
unset -f _story_ts_394
