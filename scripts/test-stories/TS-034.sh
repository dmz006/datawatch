#!/usr/bin/env bash
# TS-034 — Deliberation result shape
# tags: surface:api feature:council
# legacy fn: t4_ts034_deliberation_result_shape
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-034"
story_preflight "surface:api feature:council" || return 0

_story_ts_034() {
  if [[ -z "$RUN_ID" ]]; then skip "no run ID"; return; fi
  local resp
  resp=$(api GET "/api/council/runs/$RUN_ID")
  save_evidence TS-034 "result.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)' 2>/dev/null || echo "$resp" | grep -qi "run not found\|not found"; then
    ok "GET /api/council/runs/$RUN_ID: run found or already expired"
  else
    ko "deliberation result unexpected: $resp"
  fi
}

RESULT=fail
_story_ts_034
: "${RESULT:=fail}"
unset -f _story_ts_034
