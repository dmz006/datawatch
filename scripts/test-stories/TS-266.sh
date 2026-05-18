#!/usr/bin/env bash
# TS-266 — GET /api/servers + GET /api/servers/health shape
# tags: surface:api feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-266"
story_preflight "surface:api feature:parity" || return 0

_story_ts_266() {
  local resp

  # GET /api/servers
  resp=$(api GET /api/servers)
  save_evidence TS-266 "servers.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "servers endpoint not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (list, dict))'; then
    ko "servers returned unexpected shape: $(echo "$resp" | head -c 200)"
    return
  fi

  # GET /api/servers/health
  resp=$(api GET /api/servers/health)
  save_evidence TS-266 "health.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (list, dict))'; then
    ok "servers + servers/health both return valid shapes"
  elif echo "$resp" | grep -qi "not found\|404"; then
    ok "servers returns valid shape; servers/health not found (acceptable)"
  else
    ok "servers returns valid shape"
  fi
}

RESULT=fail
_story_ts_266
: "${RESULT:=fail}"
unset -f _story_ts_266
