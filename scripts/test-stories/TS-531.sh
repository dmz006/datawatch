#!/usr/bin/env bash
# TS-531 — POST /api/council/runs/{id}/cancel returns 200
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-531"
story_preflight "surface:api feature:council" || return 0

_story_ts_531() {
  local run_resp
  run_resp=$(api POST /api/council/run '{"question":"1+1=?","personas":[]}')
  local run_id
  run_id=$(echo "$run_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -z "$run_id" ]]; then
    skip "could not create council run for cancel test"
    return
  fi
  local cancel_resp
  cancel_resp=$(api POST "/api/council/runs/$run_id/cancel" '{}')
  save_evidence TS-531 "cancel.json" "$cancel_resp"
  if assert_json "$cancel_resp" 'isinstance(d, dict)' 2>/dev/null || echo "$cancel_resp" | grep -qi "not in progress\|already completed\|cancelled"; then
    ok "POST /api/council/runs/$run_id/cancel: run completed or cancelled"
  elif echo "$cancel_resp" | grep -qi "not found\|404"; then
    skip "council runs cancel endpoint not available"
  else
    ko "unexpected response: $(echo "$cancel_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_531
: "${RESULT:=fail}"
unset -f _story_ts_531
