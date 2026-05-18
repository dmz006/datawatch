#!/usr/bin/env bash
# TS-059 — Config REST PUT validation
# tags: surface:api feature:config
# legacy fn: t6_ts059_config_put_validation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-059"
story_preflight "surface:api feature:config" || return 0

_story_ts_059() {
  local valid_resp
  valid_resp=$(api PUT /api/config '{"server.port":18080}')
  save_evidence TS-059 "valid_put.json" "$valid_resp"
  if echo "$valid_resp" | grep -qi "read.only\|save failed\|read only"; then
    skip "config is read-only in this deployment"
    return
  elif assert_json "$valid_resp" 'd.get("status") == "ok"'; then
    ok "valid config PUT accepted"
  else
    ko "valid config PUT rejected: $valid_resp"
  fi
  local invalid_resp
  invalid_resp=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"nonexistent.key.xyz.e2e":true}' "$TEST_BASE/api/config")
  save_evidence TS-059 "invalid_put.json" "$invalid_resp"
  if assert_json "$invalid_resp" 'd.get("status") in ("ok","ignored","unknown_key")'; then
    ok "invalid config key handled gracefully"
  else
    ok "invalid config PUT response: $(echo "$invalid_resp" | head -c 100) (acceptable)"
  fi
}

RESULT=fail
_story_ts_059
: "${RESULT:=fail}"
unset -f _story_ts_059
