#!/usr/bin/env bash
# TS-058 — Config YAML reload
# tags: surface:api feature:config
# legacy fn: t6_ts058_config_reload
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-058"
story_preflight "surface:api feature:config" || return 0

_story_ts_058() {
  local full_resp
  full_resp=$(api POST /api/reload)
  save_evidence TS-058 "full_reload.json" "$full_resp"
  if assert_json "$full_resp" 'd.get("ok") and "requires_restart" in d'; then
    ok "POST /api/reload returns ok + requires_restart"
  else
    ko "reload shape wrong: $full_resp"
  fi
  local filters_resp
  filters_resp=$(curl "${curl_args[@]}" -X POST "$TEST_BASE/api/reload?subsystem=filters")
  save_evidence TS-058 "filters_reload.json" "$filters_resp"
  if assert_json "$filters_resp" 'd.get("ok") and "filters" in d.get("applied",[])'; then
    ok "reload?subsystem=filters applied"
  else
    ko "reload filters shape wrong: $filters_resp"
  fi
}

RESULT=fail
_story_ts_058
: "${RESULT:=fail}"
unset -f _story_ts_058
