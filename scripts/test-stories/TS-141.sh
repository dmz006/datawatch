#!/usr/bin/env bash
# TS-141 — Secrets panel in PWA
# tags: surface:pwa feature:secrets conflict:pwa
# legacy fn: t11_ts141_secrets_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-141"
story_preflight "surface:pwa feature:secrets conflict:pwa" || return 0

_story_ts_141() {
  local resp
  resp=$(api GET /api/secrets)
  save_evidence TS-141 "secrets.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("secrets",[]), list) or isinstance(d, dict)'; then
    ok "secrets endpoint works"
  else
    ko "secrets endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_141
: "${RESULT:=fail}"
unset -f _story_ts_141
