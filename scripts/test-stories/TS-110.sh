#!/usr/bin/env bash
# TS-110 — datawatch version
# tags: surface:cli feature:bootstrap
# legacy fn: t10_ts110_version
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-110"
story_preflight "surface:cli feature:bootstrap" || return 0

_story_ts_110() {
  local out
  out=$(cli_test version 2>&1 || true)
  save_evidence TS-110 "version.txt" "$out"
  if echo "$out" | grep -qE "v[0-9]+\.[0-9]+"; then
    ok "datawatch version: $out"
  else
    ko "version output unexpected: $out"
  fi
}

RESULT=fail
_story_ts_110
: "${RESULT:=fail}"
unset -f _story_ts_110
