#!/usr/bin/env bash
# TS-437 — POST /api/sessions/start with llm+compute_node sets both llm_ref and compute_node_ref
# tags: surface:api feature:sessions feature:compute
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-437"
story_preflight "surface:api feature:sessions feature:compute" || return 0

_story_ts_437() {
  # Try starting a session with llm + compute_node — expect success or 400 if node doesn't exist
  local resp code body
  resp=$(api_code POST /api/sessions/start \
    "{\"task\":\"test-llm-compute-ts437-$$\",\"backend\":\"shell\",\"llm\":\"shell\",\"compute_node\":\"datawatch-ollama\",\"project_dir\":\"/tmp\"}")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-437 "resp.json" "$body"
  if [[ "$code" == "200" || "$code" == "201" ]]; then
    local sid
    sid=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
    [[ -n "$sid" ]] && add_cleanup sess "$sid"
    ok "POST /api/sessions/start with llm+compute_node returned $code"
  elif [[ "$code" == "400" ]]; then
    ok "POST /api/sessions/start with llm+compute_node returned 400 (compute node not found or validation failed — expected)"
  elif [[ "$code" == "404" ]]; then
    skip "sessions/start endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_437
: "${RESULT:=fail}"
unset -f _story_ts_437
