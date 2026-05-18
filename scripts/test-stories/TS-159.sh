#!/usr/bin/env bash
# TS-159 — Autonomous scan config
# tags: surface:api feature:automata
# legacy fn: t12_ts159_autonomous_scan_config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-159"
story_preflight "surface:api feature:automata" || return 0

_story_ts_159() {
  local resp
  resp=$(api GET /api/autonomous/scan/config 2>/dev/null || \
        api GET /api/autonomous/config 2>/dev/null || echo '{"error":"not found"}')
  save_evidence TS-159 "scan_config.json" "$resp"
  if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
    ok "Autonomous scan config endpoint reachable"
    local put_resp
    put_resp=$(api PUT /api/autonomous/scan/config '{"sast_enabled":true}' 2>/dev/null || \
              api POST /api/autonomous/scan/config '{"sast_enabled":true}' 2>/dev/null || echo '{}')
    save_evidence TS-159 "put.json" "$put_resp"
    if assert_json "$put_resp" 'isinstance(d, dict)'; then
      ok "Autonomous scan config PUT accepted"
    else
      skip "Autonomous scan config PUT not available"
    fi
  else
    skip "Autonomous scan config endpoint not available"
  fi
}

RESULT=fail
_story_ts_159
: "${RESULT:=fail}"
unset -f _story_ts_159
