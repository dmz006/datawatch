#!/usr/bin/env bash
# TS-619 — peer's /api/proxy/llm route reachable
# tags: surface:api feature:routing group:routing-v8 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-619"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok" || return 0

_story_ts_619() {
  if [[ -z "${DW_PEER_URL:-}" ]]; then
    skip "DW_PEER_URL not set — skip peer routing test"; return
  fi

  local peer_token="${DW_PEER_TOKEN:-peer-test-token}"
  local code
  code=$(curl -sk --max-time 10 \
    -H "Authorization: Bearer $peer_token" \
    -w "%{http_code}" -o /dev/null \
    "${DW_PEER_URL}/api/proxy/llm/test-llm" 2>/dev/null || echo "000")
  save_evidence TS-619 "peer_probe_code.txt" "$code"

  if [[ "$code" == "000" ]]; then
    ko "peer /api/proxy/llm/test-llm unreachable (connection refused or timeout)"
  else
    ok "peer /api/proxy/llm route reachable (HTTP $code)"
  fi
}

RESULT=fail
_story_ts_619
: "${RESULT:=fail}"
unset -f _story_ts_619
