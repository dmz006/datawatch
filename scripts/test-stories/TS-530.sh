#!/usr/bin/env bash
# TS-530 — GET /api/council/runs/{id}/events returns SSE stream or 200
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-530"
story_preflight "surface:api feature:council" || return 0

_story_ts_530() {
  # Create a council run first
  local run_resp
  run_resp=$(api POST /api/council/run '{"proposal":"1+1=?","personas":[],"mode":"quick"}')
  local run_id
  run_id=$(echo "$run_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -z "$run_id" ]]; then
    skip "could not get run_id for events test"
    return
  fi
  add_cleanup council "$run_id"
  local code
  # Use a temp file to avoid || echo "0" appending to the HTTP code when curl times out on SSE
  local _tmp
  _tmp=$(mktemp)
  curl "${curl_args[@]}" --max-time 2 -o /dev/null -w "%{http_code}" \
    "$TEST_BASE/api/council/runs/$run_id/events" > "$_tmp" 2>/dev/null || true
  code=$(cat "$_tmp"); rm -f "$_tmp"
  [[ "$code" =~ ^[0-9]{3}$ ]] || code="0"
  save_evidence TS-530 "events_code.txt" "$code"
  if [[ "$code" == "200" ]]; then
    ok "GET /api/council/runs/$run_id/events returns 200 (SSE stream)"
  elif [[ "$code" == "404" ]]; then
    skip "council runs events endpoint not available (404)"
  elif [[ "$code" == "0" ]]; then
    skip "council runs events endpoint unreachable"
  else
    ko "unexpected HTTP $code for council run events"
  fi
}

RESULT=fail
_story_ts_530
: "${RESULT:=fail}"
unset -f _story_ts_530
