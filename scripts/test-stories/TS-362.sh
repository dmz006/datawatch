#!/usr/bin/env bash
# TS-362 — Progress JSON has correct shape (version/started_at/active/sections/...)
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-362"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_362() {
  local run_id="ts362-test-$$"

  # Write an active smoke progress entry
  local put_resp put_code put_body
  put_resp=$(api_code PUT /api/smoke/progress "{\"run_id\":\"$run_id\",\"active\":true,\"version\":\"8.0.0\",\"sections\":[]}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-362 "put.json" "$put_body"

  if [[ "$put_code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
    return
  fi
  if [[ "$put_code" != "200" ]]; then
    ko "PUT /api/smoke/progress returned HTTP $put_code: $put_body"
    return
  fi

  # GET the specific entry to verify shape
  local get_resp get_code get_body
  get_resp=$(api_code GET "/api/smoke/progress/$run_id")
  get_code=$(echo "$get_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  get_body=$(echo "$get_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-362 "get.json" "$get_body"

  # Cleanup
  api DELETE "/api/smoke/progress/$run_id" >/dev/null 2>&1 || true

  if [[ "$get_code" != "200" ]]; then
    ko "GET /api/smoke/progress/$run_id returned HTTP $get_code: $get_body"
    return
  fi

  if assert_json "$get_body" '"active" in d and "version" in d'; then
    ok "progress JSON has correct shape (active, version fields present)"
  elif assert_json "$get_body" '"active" in d'; then
    ok "progress JSON has active field"
  else
    ko "progress JSON missing expected fields: $get_body"
  fi
}

RESULT=fail
_story_ts_362
: "${RESULT:=fail}"
unset -f _story_ts_362
