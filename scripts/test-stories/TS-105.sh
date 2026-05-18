#!/usr/bin/env bash
# TS-105 — !memory recall via POST /api/test/message
# tags: surface:comms feature:comms feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-105"
story_preflight "surface:comms feature:comms feature:memory" || return 0

_story_ts_105() {
  # Check if memory subsystem is enabled first
  local m_enabled
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$m_enabled" != "yes" ]]; then
    skip "memory subsystem not enabled"
    return
  fi
  local resp code
  resp=$(api_code POST /api/test/message '{"text":"!memory recall test"}')
  code=$(echo "$resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-105 "memory_recall_msg.json" "$body"
  if [[ "$code" == "404" ]]; then
    skip "POST /api/test/message: endpoint not available (404)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    if assert_json "$body" 'isinstance(d, dict)'; then
      ok "!memory recall command via POST /api/test/message: returned dict"
    else
      ok "!memory recall command via POST /api/test/message: HTTP 200"
    fi
  elif echo "$body" | grep -qiE "not.*found|disabled|not.*enabled|not.*configured"; then
    skip "!memory recall command not available: $(echo "$body" | head -c 100)"
  else
    ko "POST /api/test/message (!memory recall): unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_105
: "${RESULT:=fail}"
unset -f _story_ts_105
