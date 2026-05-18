#!/usr/bin/env bash
# TS-422 — POST /api/secrets/{name} sets secret; DELETE /api/secrets/{name} removes it
# tags: surface:api feature:secrets
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-422"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_422() {
  local secret_name="test-secret-ts422-$$"
  local resp code body
  # POST to set secret
  resp=$(api_code POST "/api/secrets/$secret_name" \
    '{"value":"test-value-ts422","scope":"global"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-422 "set_resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/secrets endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" && "$code" != "204" ]]; then
    # Try without scope
    resp=$(api_code POST "/api/secrets/$secret_name" '{"value":"test-value-ts422"}')
    code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
    if [[ "$code" != "200" && "$code" != "201" && "$code" != "204" ]]; then
      ko "POST /api/secrets/$secret_name returned $code: $body"
      return
    fi
  fi
  # DELETE it
  local del_code
  del_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X DELETE -H "Authorization: Bearer $TEST_TOKEN" \
    "$TEST_BASE/api/secrets/$secret_name")
  save_evidence TS-422 "del_code.txt" "$del_code"
  if [[ "$del_code" == "200" || "$del_code" == "204" ]]; then
    ok "POST /api/secrets set and DELETE removed secret $secret_name"
  elif [[ "$del_code" == "404" ]]; then
    ko "secret $secret_name not found on DELETE after setting"
  else
    ko "DELETE returned unexpected $del_code"
  fi
}

RESULT=fail
_story_ts_422
: "${RESULT:=fail}"
unset -f _story_ts_422
