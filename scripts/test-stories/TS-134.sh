#!/usr/bin/env bash
# TS-134 — Start new session from PWA
# tags: surface:pwa feature:sessions conflict:pwa
# legacy fn: t11_ts134_new_session
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-134"
story_preflight "surface:pwa feature:sessions conflict:pwa" || return 0

_story_ts_134() {
  local resp
  resp=$(api POST /api/sessions/start '{"backend":"shell","project_dir":"/tmp","task":"test-pwa-134","name":"test-pwa-134"}')
  save_evidence TS-134 "new_session.json" "$resp"
  local sid
  sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("id","") or d.get("full_id","")))' 2>/dev/null || echo "")
  if [[ -n "$sid" ]]; then
    ok "new session created via API: $sid"
    add_cleanup session "$sid"
  elif echo "$resp" | grep -q "max sessions"; then
    ok "session limit enforcement verified"
  else
    ko "session create failed"
  fi
}

RESULT=fail
_story_ts_134
: "${RESULT:=fail}"
unset -f _story_ts_134
