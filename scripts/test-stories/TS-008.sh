#!/usr/bin/env bash
# TS-008 — Diagnose endpoint
# tags: surface:api feature:bootstrap
# legacy fn: t1_ts008_diagnose
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-008"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_008() {
  local diag
  diag=$(api GET /api/diagnose)
  save_evidence TS-008 "diagnose.json" "$diag"
  if assert_json "$diag" 'isinstance(d, (dict, list))'; then
    ok "GET /api/diagnose returns valid JSON"
  else
    ko "diagnose unexpected: $(echo "$diag" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_008
: "${RESULT:=fail}"
unset -f _story_ts_008
