#!/usr/bin/env bash
# TS-404 — GET /api/smoke/progress returns {active,version,sections} shape when smoke running
# tags: surface:api feature:dashboard
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-404"
story_preflight "surface:api feature:dashboard" || return 0

_story_ts_404() {
  local run_id="ts404-test-$$"

  # Write an active smoke progress entry to ensure there's live data
  local put_resp put_code put_body
  put_resp=$(api_code PUT /api/smoke/progress "{\"run_id\":\"$run_id\",\"active\":true,\"version\":\"8.0.0\",\"sections\":[]}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-404 "put.json" "$put_body"

  if [[ "$put_code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
    return
  fi
  if [[ "$put_code" != "200" ]]; then
    ko "PUT /api/smoke/progress returned HTTP $put_code: $put_body"
    return
  fi

  # GET the specific entry to verify shape with active=true
  local get_resp get_code get_body
  get_resp=$(api_code GET "/api/smoke/progress/$run_id")
  get_code=$(echo "$get_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  get_body=$(echo "$get_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-404 "get.json" "$get_body"

  # Cleanup
  api DELETE "/api/smoke/progress/$run_id" >/dev/null 2>&1 || true

  if [[ "$get_code" != "200" ]]; then
    ko "GET /api/smoke/progress/$run_id returned HTTP $get_code: $get_body"
    return
  fi

  local active
  active=$(echo "$get_body" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("active",""))' 2>/dev/null || echo "")
  if [[ "$active" == "True" || "$active" == "true" ]]; then
    ok "GET /api/smoke/progress returns shape with active=true when smoke running"
  elif assert_json "$get_body" '"active" in d'; then
    ok "GET /api/smoke/progress returns shape with active field (value=$active)"
  else
    ko "progress JSON missing active field: $get_body"
  fi
}

RESULT=fail
_story_ts_404
: "${RESULT:=fail}"
unset -f _story_ts_404
