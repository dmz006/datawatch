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
  # PATCH to update a field
  local patch_resp patch_code patch_body
  patch_resp=$(api_code PATCH "/api/llms/$llm_name" \
    '{"enabled":false}')
  patch_code=$(echo "$patch_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  patch_body=$(echo "$patch_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-372 "patch_resp.json" "$patch_body"
  if [[ "$patch_code" == "200" || "$patch_code" == "204" ]]; then
    ok "PATCH /api/llms/$llm_name returned $patch_code"
  elif [[ "$patch_code" == "404" ]]; then
    skip "PATCH /api/llms endpoint not available (404)"
  elif [[ "$patch_code" == "405" ]]; then
    skip "PATCH method not supported for /api/llms (405)"
  else
    ko "unexpected PATCH $patch_code: $patch_body"
  fi
}

RESULT=fail
_story_ts_372
: "${RESULT:=fail}"
unset -f _story_ts_372
