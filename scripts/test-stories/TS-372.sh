#!/usr/bin/env bash
# TS-372 — LLM PATCH session field update
# tags: surface:api feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-372"
story_preflight "surface:api feature:config" || return 0

_story_ts_372() {
  local llm_name="test-llm-ts372-$$"
  # Create LLM first
  local create_resp code
  create_resp=$(api_code POST /api/llms \
    "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":true}")
  code=$(echo "$create_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "could not create LLM for PATCH test: $code"
    return
  fi
  add_cleanup llm "$llm_name"
  # PUT to update a field (API uses PUT, not PATCH)
  local put_resp put_code put_body
  put_resp=$(api_code PUT "/api/llms/$llm_name" \
    "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":false}")
  put_code=$(echo "$put_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  put_body=$(echo "$put_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-372 "put_resp.json" "$put_body"
  if [[ "$put_code" == "200" || "$put_code" == "204" ]]; then
    ok "PUT /api/llms/$llm_name returned $put_code"
  elif [[ "$put_code" == "404" ]]; then
    skip "PUT /api/llms endpoint not available (404)"
  else
    ko "unexpected PUT $put_code: $put_body"
  fi
}

RESULT=fail
_story_ts_372
: "${RESULT:=fail}"
unset -f _story_ts_372
