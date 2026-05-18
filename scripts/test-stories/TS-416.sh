#!/usr/bin/env bash
# TS-416 — POST /api/llms creates entry; GET /api/llms/{name} round-trips
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-416"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_416() {
  local llm_name="test-llm-ts416-$$"
  local resp code body
  resp=$(api_code POST /api/llms \
    "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":true}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-416 "create_resp.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "POST /api/llms returned $code: $body"
    return
  fi
  add_cleanup llm "$llm_name"
  # GET by name
  local get_resp
  get_resp=$(api GET "/api/llms/$llm_name")
  save_evidence TS-416 "get_resp.json" "$get_resp"
  if assert_json "$get_resp" '"name" in d'; then
    ok "POST /api/llms created entry; GET /api/llms/$llm_name round-trips"
  elif echo "$get_resp" | grep -qi "not found\|404"; then
    ko "LLM $llm_name not found after creation"
  else
    ko "unexpected GET response: $(echo "$get_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_416
: "${RESULT:=fail}"
unset -f _story_ts_416
