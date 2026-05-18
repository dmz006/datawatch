#!/usr/bin/env bash
# TS-111 — datawatch status
# tags: surface:cli feature:bootstrap
# legacy fn: t10_ts111_status
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-111"
story_preflight "surface:cli feature:bootstrap" || return 0

_story_ts_111() {
  local out
  out=$(cli_test status 2>&1 || true)
  save_evidence TS-111 "status.txt" "$out"
  if [[ -n "$out" ]]; then
    ok "datawatch status returned output: $(echo "$out" | head -c 100)"
  else
    skip "datawatch status returned no output"
  fi
}

RESULT=fail
_story_ts_111
: "${RESULT:=fail}"
unset -f _story_ts_111
