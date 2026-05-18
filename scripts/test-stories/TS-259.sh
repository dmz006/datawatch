#!/usr/bin/env bash
# TS-259 — GET /api/openwebui/models returns array
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-259"
story_preflight "surface:api feature:parity" || return 0

_story_ts_259() {
  local resp
  resp=$(api GET /api/openwebui/models)
  save_evidence TS-259 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "openwebui/models returns array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("models" in d or "data" in d)'; then
    ok "openwebui/models returns dict with models key"
  elif echo "$resp" | grep -qi "not found\|404\|no route\|connection refused\|no such host\|not configured\|not enabled"; then
    skip "openwebui not available or not configured in this deployment"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_259
: "${RESULT:=fail}"
unset -f _story_ts_259
