#!/usr/bin/env bash
# TS-610 — Invalid routing value rejected (expect 400)
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-610"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_610() {
  local payload resp code
  payload='{"name":"r610-invalid","kind":"ollama","address":"http://localhost:11434","routing":"teleportation"}'
  resp=$(api_code POST /api/compute/nodes "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-610 "create_invalid.json" "$resp"

  if [[ "$code" == "400" ]]; then
    ok "invalid routing value correctly rejected with 400"
  else
    ko "expected 400 for invalid routing value, got $code"
  fi
}

RESULT=fail
_story_ts_610
: "${RESULT:=fail}"
unset -f _story_ts_610
