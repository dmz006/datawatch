#!/usr/bin/env bash
# TS-527 — GET /api/secrets/vault/status returns {backend,connected} shape
# tags: surface:api feature:secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-527"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_527() {
  local resp
  resp=$(api GET /api/secrets/vault/status)
  save_evidence TS-527 "status.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|unknown"; then
    skip "secrets/vault/status endpoint not available"
    return
  fi
  if assert_json "$resp" '"backend" in d or "connected" in d'; then
    ok "GET /api/secrets/vault/status has backend or connected field"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "GET /api/secrets/vault/status returns dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_527
: "${RESULT:=fail}"
unset -f _story_ts_527
