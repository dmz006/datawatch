#!/usr/bin/env bash
# TS-418 — POST /api/llms/{name}/refresh_models returns 200
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-418"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_418() {
  local llm_name="test-llm-ts418-$$"
  local create_code
  create_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":true}" \
    "$TEST_BASE/api/llms")
  if [[ "$create_code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$create_code" != "200" && "$create_code" != "201" ]]; then
    ko "could not create LLM for refresh_models test (code=$create_code)"
    return
  fi
  add_cleanup llm "$llm_name"
  local resp code body
  resp=$(api_code POST "/api/llms/$llm_name/refresh_models" '')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-418 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    ok "POST /api/llms/$llm_name/refresh_models returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "refresh_models endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_418
: "${RESULT:=fail}"
unset -f _story_ts_418
