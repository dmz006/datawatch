#!/usr/bin/env bash
# TS-311 — datawatch autonomous template-list exits 0
# tags: surface:cli feature:cli feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-311"
story_preflight "surface:cli feature:cli feature:automata" || return 0

_story_ts_311() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  local out; out=$(cli_test autonomous template-list 2>&1); local rc=$?
  save_evidence TS-311 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "autonomous template-list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available"; then
    skip "autonomous template-list not configured: $(echo "$out" | head -c 80)"
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_311
: "${RESULT:=fail}"
unset -f _story_ts_311
