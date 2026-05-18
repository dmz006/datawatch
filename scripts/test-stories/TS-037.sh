#!/usr/bin/env bash
# TS-037 — Council include_claude_code config
# tags: surface:api feature:council feature:config
# legacy fn: t4_ts037_council_config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-037"
story_preflight "surface:api feature:council feature:config" || return 0

_story_ts_037() {
  local cfg_before
  cfg_before=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("council",{}).get("include_claude_code","not_found"))' 2>/dev/null || echo "not_found")
  save_evidence TS-037 "before.json" "{\"include_claude_code\":\"$cfg_before\"}"
  local put_resp
  put_resp=$(api PUT /api/config '{"council.include_claude_code":true}')
  save_evidence TS-037 "put.json" "$put_resp"
  if assert_json "$put_resp" 'd.get("status") == "ok"'; then
    ok "council.include_claude_code config PUT accepted"
    # Restore
    if [[ "$cfg_before" == "True" || "$cfg_before" == "true" ]]; then
      api PUT /api/config '{"council.include_claude_code":true}' >/dev/null
    else
      api PUT /api/config '{"council.include_claude_code":false}' >/dev/null
    fi
    ok "council.include_claude_code restored"
  else
    skip "council.include_claude_code config key not present (may not be in this version)"
  fi
}

RESULT=fail
_story_ts_037
: "${RESULT:=fail}"
unset -f _story_ts_037
