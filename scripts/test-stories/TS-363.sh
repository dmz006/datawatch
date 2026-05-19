#!/usr/bin/env bash
# TS-363 — After smoke completes, active=false in progress JSON
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-363"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_363() {
  local run_id="ts363-test-$$"

  # Write a completed (active=false) smoke progress entry
  local put_resp put_code put_body
  put_resp=$(api_code PUT /api/smoke/progress "{\"run_id\":\"$run_id\",\"active\":false,\"version\":\"8.0.0\",\"sections\":[]}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-363 "put.json" "$put_body"

  if [[ "$put_code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
    return
  fi
  if [[ "$put_code" != "200" ]]; then
    ko "PUT /api/smoke/progress returned HTTP $put_code: $put_body"
    return
  fi

  # GET the specific entry to verify active=false
  local get_resp get_code get_body
  get_resp=$(api_code GET "/api/smoke/progress/$run_id")
  get_code=$(echo "$get_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  get_body=$(echo "$get_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-363 "get.json" "$get_body"

  # Cleanup
  api DELETE "/api/smoke/progress/$run_id" >/dev/null 2>&1 || true

  if [[ "$get_code" != "200" ]]; then
    ko "GET /api/smoke/progress/$run_id returned HTTP $get_code: $get_body"
    return
  fi

  local active
  active=$(echo "$get_body" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("active",""))' 2>/dev/null || echo "")
  if [[ "$active" == "False" || "$active" == "false" ]]; then
    ok "active=false correctly stored and returned after smoke completion"
  else
    ko "active field expected false, got '$active': $get_body"
  fi
}

RESULT=fail
_story_ts_363
: "${RESULT:=fail}"
unset -f _story_ts_363
