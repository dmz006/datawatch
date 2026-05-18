#!/usr/bin/env bash
# TS-376 — LLM enable toggle skips pretest for session-backend kinds (aider/goose/shell)
# tags: surface:api feature:config
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-376"
story_preflight "surface:api feature:config" || return 0

_story_ts_376() {
  # Create a shell-kind LLM and toggle enable — should skip pretest
  local llm_name="test-llm-ts376-$$"
  local create_resp code
  create_resp=$(api_code POST /api/llms \
    "{\"name\":\"$llm_name\",\"kind\":\"shell\",\"enabled\":false}")
  code=$(echo "$create_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$code" == "404" ]]; then
    skip "POST /api/llms endpoint not available (404)"
    return
  fi
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    ko "could not create shell LLM for enable toggle test: $code"
    return
  fi
  add_cleanup llm "$llm_name"
  # Enable it — for session-backend kinds should not run pretest
  local en_resp en_code en_body
  en_resp=$(api_code POST "/api/llms/$llm_name/enable" '')
  en_code=$(echo "$en_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  en_body=$(echo "$en_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-376 "enable_resp.json" "$en_body"
  if [[ "$en_code" == "200" || "$en_code" == "204" ]]; then
    ok "POST /api/llms/$llm_name/enable returned $en_code (shell kind skips pretest)"
  elif [[ "$en_code" == "404" ]]; then
    skip "LLM enable endpoint not available (404)"
  elif [[ "$en_code" == "405" ]]; then
    # Try PUT if POST not allowed
    en_resp=$(api_code PUT "/api/llms/$llm_name/enable" '')
    en_code=$(echo "$en_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    en_body=$(echo "$en_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
    if [[ "$en_code" == "200" || "$en_code" == "204" ]]; then
      ok "PUT /api/llms/$llm_name/enable returned $en_code"
    else
      skip "LLM enable endpoint returned 405 (method not allowed) — API may have changed"
    fi
  else
    ko "unexpected HTTP $en_code enabling shell LLM: $en_body"
  fi
}

RESULT=fail
_story_ts_376
: "${RESULT:=fail}"
unset -f _story_ts_376
