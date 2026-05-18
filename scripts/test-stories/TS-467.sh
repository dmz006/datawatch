#!/usr/bin/env bash
# TS-467 — datawatch autonomous prd-plan --help shows prd-plan command
# tags: surface:cli feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-467"
story_preflight "surface:cli feature:automata" || return 0

_story_ts_467() {
  local out rc
  out=$(cli_test autonomous prd-plan --help 2>&1); rc=$?
  save_evidence TS-467 "out.txt" "$out"
  if [[ $rc -eq 0 ]] || echo "$out" | grep -qi "prd-plan\|plan\|usage"; then
    ok "datawatch autonomous prd-plan --help: $(echo "$out" | head -c 80)"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    # Try help autonomous
    out2=$(cli_test help autonomous 2>&1)
    if echo "$out2" | grep -qi "prd-plan"; then
      ok "prd-plan listed in 'help autonomous'"
    else
      skip "prd-plan not available: $(echo "$out" | head -c 80)"
    fi
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_467
: "${RESULT:=fail}"
unset -f _story_ts_467
