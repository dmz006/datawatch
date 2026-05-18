#!/usr/bin/env bash
# TS-006 — Config GET round-trip
# tags: surface:api feature:bootstrap feature:config blocking
# legacy fn: t1_ts006_config_get
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-006"
story_preflight "surface:api feature:bootstrap feature:config blocking" || return 0

_story_ts_006() {
  local cfg
  cfg=$(api GET /api/config)
  save_evidence TS-006 "config.json" "$cfg"
  if assert_json "$cfg" '"server" in d or "session" in d'; then
    ok "GET /api/config returns top-level sections"
  else
    ko "config shape unexpected: $(echo "$cfg" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_006
: "${RESULT:=fail}"
unset -f _story_ts_006
