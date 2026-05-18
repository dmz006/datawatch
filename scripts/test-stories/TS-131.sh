#!/usr/bin/env bash
# TS-131 — Auth token accepted
# tags: surface:pwa feature:bootstrap conflict:pwa
# legacy fn: t11_ts131_auth_token
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-131"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_131() {
  local resp
  resp=$(api POST /api/sessions/start '{"backend":"shell","project_dir":"/tmp","task":"test-pwa-131","name":"test-pwa-131"}')
  save_evidence TS-131 "session.json" "$resp"
  local sid
  sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("id","") or d.get("full_id","")))' 2>/dev/null || echo "")
  if [[ -n "$sid" ]]; then
    ok "auth token accepted, session created: $sid"
    add_cleanup session "$sid"
  elif echo "$resp" | grep -q "max sessions"; then
    ok "session limit enforcement verified (auth working): $(echo $resp | head -c 50)"
  else
    ko "auth token failed or session creation failed"
  fi
}

RESULT=fail
_story_ts_131
: "${RESULT:=fail}"
unset -f _story_ts_131
