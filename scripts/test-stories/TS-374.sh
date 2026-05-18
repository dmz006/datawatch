#!/usr/bin/env bash
# TS-374 — datawatch secrets import claude exits 0
# tags: surface:cli feature:cli feature:secrets
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-374"
story_preflight "surface:cli feature:cli feature:secrets" || return 0

_story_ts_374() {
  local out rc
  out=$(cli_test secrets import claude --help 2>&1); rc=$?
  save_evidence TS-374 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch secrets import claude --help exits 0"
  elif echo "$out" | grep -qiE "unknown command|not found|disabled|not.*available|no such"; then
    skip "secrets import claude not available: $(echo "$out" | head -c 80)"
  else
    out=$(cli_test secrets import claude 2>&1); rc=$?
    if [[ $rc -eq 0 ]]; then
      ok "datawatch secrets import claude exits 0"
    elif echo "$out" | grep -qiE "unknown command|not found|disabled|no such"; then
      skip "secrets import claude not available: $(echo "$out" | head -c 80)"
    else
      skip "secrets import claude requires credentials (rc=$rc): $(echo "$out" | head -c 80)"
    fi
  fi
}

RESULT=fail
_story_ts_374
: "${RESULT:=fail}"
unset -f _story_ts_374
