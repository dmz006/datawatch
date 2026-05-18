#!/usr/bin/env bash
# TS-617 — datawatch-proxy routing — missing peer field returns 400
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-617"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_617() {
  local payload resp code
  payload='{"name":"r617-proxy-nopeer","kind":"ollama","address":"http://localhost:11434","routing":"datawatch-proxy","routing_datawatch_proxy":{"remote_llm_name":"test-llm"}}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-617 "create_no_peer.json" "$resp"

  if [[ "$code" == "400" ]]; then
    ok "datawatch-proxy node without peer field correctly rejected with 400"
  else
    ko "expected 400 for missing peer field, got $code"
  fi
}

RESULT=fail
_story_ts_617
: "${RESULT:=fail}"
unset -f _story_ts_617
