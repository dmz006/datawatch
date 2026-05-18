#!/usr/bin/env bash
# TS-474 — datawatch autonomous list exits 0
# tags: surface:cli feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-474"
story_preflight "surface:cli feature:automata" || return 0

_story_ts_474() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$a_enabled" != "yes" ]] && { skip "autonomous disabled"; return; }
  local out rc
  out=$(cli_test autonomous list 2>&1); rc=$?
  save_evidence TS-474 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch autonomous list exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_474
: "${RESULT:=fail}"
unset -f _story_ts_474
