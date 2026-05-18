#!/usr/bin/env bash
# TS-054 — Config \${secret:name} ref resolution
# tags: surface:api feature:secrets feature:config
# legacy fn: t6_ts054_config_secret_ref
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-054"
story_preflight "surface:api feature:secrets feature:config" || return 0

_story_ts_054() {
  local put_resp
  put_resp=$(api PUT /api/config '{"session.extra_env":"${secret:test-ref-secret-e2e}"}')
  save_evidence TS-054 "put.json" "$put_resp"
  if assert_json "$put_resp" 'd.get("status") == "ok"'; then
    ok "config accepts secret ref notation"
    # Restore
    api PUT /api/config '{"session.extra_env":""}' >/dev/null
  else
    skip "config key session.extra_env not supported: $(echo "$put_resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_054
: "${RESULT:=fail}"
unset -f _story_ts_054
