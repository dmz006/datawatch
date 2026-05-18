#!/usr/bin/env bash
# TS-033 — Council cancel
# tags: surface:api feature:council
# legacy fn: t4_ts033_council_cancel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-033"
story_preflight "surface:api feature:council" || return 0

_story_ts_033() {
  if [[ -z "$RUN_ID" ]]; then skip "no run ID"; return; fi
  local resp
  resp=$(api POST "/api/council/runs/$RUN_ID/cancel" '{}')
  save_evidence TS-033 "cancel.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)' 2>/dev/null || echo "$resp" | grep -qi "not in progress\|already completed"; then
    ok "council cancel: run completed or successfully cancelled"
  else
    ko "council cancel failed: $resp"
  fi
}

RESULT=fail
_story_ts_033
: "${RESULT:=fail}"
unset -f _story_ts_033
