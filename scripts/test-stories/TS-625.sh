#!/usr/bin/env bash
# TS-625 — probe=skip query param bypasses connectivity check
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-625"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_625() {
  local payload resp code
  payload='{"name":"r625-noprobe","kind":"ollama","address":"http://127.0.0.1:19999","routing":"direct"}'
  resp=$(api_code POST /api/compute/nodes?probe=skip "$payload")
  code=$(echo "$resp" | grep -o '__HTTP_CODE_[0-9]*__' | tr -d '_' | sed 's/HTTP_CODE_//')
  save_evidence TS-625 "create_probe_skip.json" "$resp"

  if [[ "$code" == "200" || "$code" == "201" ]]; then
    ok "probe=skip bypasses connectivity check: node created with unreachable address (HTTP $code)"
    add_cleanup compute_node "r625-noprobe"
    api DELETE /api/compute/nodes/r625-noprobe >/dev/null 2>&1
  else
    ko "expected 200/201 with probe=skip for unreachable address, got $code"
  fi
}

RESULT=fail
_story_ts_625
: "${RESULT:=fail}"
unset -f _story_ts_625
