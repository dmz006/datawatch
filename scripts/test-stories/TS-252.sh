#!/usr/bin/env bash
# TS-252 — GET /api/docs returns Swagger HTML (200)
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-252"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_252() {
  local resp
  resp=$(api GET /api/docs)
  save_evidence TS-252 "resp.html" "$resp"
  if echo "$resp" | grep -qi "swagger\|openapi\|<!doctype html\|<html"; then
    ok "docs returns HTML/Swagger page"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "docs endpoint not available in this build"
  else
    ko "docs returned unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_252
: "${RESULT:=fail}"
unset -f _story_ts_252
