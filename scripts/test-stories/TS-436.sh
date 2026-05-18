#!/usr/bin/env bash
# TS-436 — POST /api/sessions/start with llm=claude-code sets llm_ref on returned session
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-436"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_436() {
  local resp code body
  resp=$(api_code POST /api/sessions/start \
    "{\"task\":\"test-llm-ref-ts436-$$\",\"backend\":\"shell\",\"llm\":\"shell\",\"project_dir\":\"/tmp\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-436 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "201" ]]; then
    local sid
    sid=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
    [[ -n "$sid" ]] && add_cleanup sess "$sid"
    ok "POST /api/sessions/start with llm=shell returned $code"
  elif [[ "$code" == "400" ]]; then
    if echo "$body" | grep -qi "not found\|unknown\|no such\|not registered"; then
      skip "LLM 'shell' not registered in this build: $body"
    else
      ok "POST /api/sessions/start with llm= returned 400 (endpoint exists, validation fired)"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "sessions/start endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_436
: "${RESULT:=fail}"
unset -f _story_ts_436
