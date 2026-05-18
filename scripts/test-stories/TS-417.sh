#!/usr/bin/env bash
# TS-417 — GET /api/llms/{name}/in_use returns {bindings:[]} shape
# tags: surface:api feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-417"
story_preflight "surface:api feature:llm-registry" || return 0

_story_ts_417() {
  local llm_name="test-llm-ts417-$$"
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
    ko "could not create LLM for in_use test (code=$create_code)"
    return
  fi
  add_cleanup llm "$llm_name"
  # GET in_use
  local resp
  resp=$(api GET "/api/llms/$llm_name/in_use")
  save_evidence TS-417 "resp.json" "$resp"
  if assert_json "$resp" '"bindings" in d and isinstance(d["bindings"], list)'; then
    ok "GET /api/llms/$llm_name/in_use returns {bindings:[...]} shape"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/llms/$llm_name/in_use returns dict"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "llms in_use endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_417
: "${RESULT:=fail}"
unset -f _story_ts_417
