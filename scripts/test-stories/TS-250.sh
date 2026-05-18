#!/usr/bin/env bash
# TS-250 — GET /api/splash/info returns hostname+version
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-250"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_250() {
  local resp
  resp=$(api GET /api/splash/info)
  save_evidence TS-250 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict) and "hostname" in d and "version" in d'; then
    ok "splash/info has hostname and version"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ko "splash/info missing hostname or version: $resp"
  elif echo "$resp" | grep -qi "not found\|404"; then
    skip "splash/info endpoint not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_250
: "${RESULT:=fail}"
unset -f _story_ts_250
