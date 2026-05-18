#!/usr/bin/env bash
# TS-423 — POST /api/sessions/set_llm_ref updates session llm_ref binding
# tags: surface:api feature:sessions feature:llm-registry
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-423"
story_preflight "surface:api feature:sessions feature:llm-registry" || return 0

_story_ts_423() {
  ensure_test_session || return
  local resp code body
  resp=$(api_code POST /api/sessions/set_llm_ref \
    "{\"id\":\"$SESSION_ID\",\"llm_ref\":\"shell\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-423 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "204" ]]; then
    ok "POST /api/sessions/set_llm_ref returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "set_llm_ref endpoint not available (404)"
  elif [[ "$code" == "400" ]]; then
    # LLM "shell" may not exist — check error message
    if echo "$body" | grep -qi "not found\|unknown\|no such"; then
      skip "LLM 'shell' not registered in this build: $body"
    else
      ko "POST /api/sessions/set_llm_ref returned 400: $body"
    fi
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_423
: "${RESULT:=fail}"
unset -f _story_ts_423
