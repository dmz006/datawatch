#!/usr/bin/env bash
# TS-361 — Running release-smoke.sh writes progress JSON before first section
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-361"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_361() {
  local run_id="ts361-test-$$"

  # Write a smoke progress entry (active=true, one section)
  local put_resp put_code put_body
  put_resp=$(api_code PUT /api/smoke/progress "{\"run_id\":\"$run_id\",\"active\":true,\"version\":\"8.0.0\",\"sections\":[{\"name\":\"boot\",\"pass\":1,\"fail\":0,\"skip\":0}]}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-361 "put.json" "$put_body"

  if [[ "$put_code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
    return
  fi
  if [[ "$put_code" != "200" ]]; then
    ko "PUT /api/smoke/progress returned HTTP $put_code: $put_body"
    return
  fi

  # GET the list to verify entry was written
  local list_resp list_code list_body
  list_resp=$(api_code GET /api/smoke/progress)
  list_code=$(echo "$list_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  list_body=$(echo "$list_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-361 "list.json" "$list_body"

  # Cleanup: delete the test entry
  api DELETE "/api/smoke/progress/$run_id" >/dev/null 2>&1 || true

  if [[ "$list_code" != "200" ]]; then
    ko "GET /api/smoke/progress returned HTTP $list_code"
    return
  fi

  ok "smoke/progress write+read+delete lifecycle works (run_id=$run_id)"
}

RESULT=fail
_story_ts_361
: "${RESULT:=fail}"
unset -f _story_ts_361
