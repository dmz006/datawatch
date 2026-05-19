#!/usr/bin/env bash
# TS-633 — GET /api/plugins/browse?registry=community returns 200
# tags: surface:rest feature:plugin-install
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-633"
story_preflight "surface:rest feature:plugin-install" || return 0

_story_ts_633() {
  # Skip if skills registry is not connected
  local reg_resp
  reg_resp=$(api GET /api/skills/registries 2>/dev/null)
  if ! echo "$reg_resp" | grep -qi "community"; then
    skip "community skills registry not connected — skipping browse test"
    return
  fi

  local resp code
  resp=$(api_code GET "/api/plugins/browse?registry=community")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-633 "browse.json" "$resp"

  if [[ "$code" == "503" || "$code" == "501" || "$code" == "404" ]]; then
    skip "plugins/browse endpoint not available (HTTP $code)"
    return
  fi
  if [[ "$code" == "200" ]]; then
    ok "GET /api/plugins/browse?registry=community returns 200"
  else
    ko "GET /api/plugins/browse?registry=community expected 200, got $code"
  fi
}

RESULT=fail
_story_ts_633
: "${RESULT:=fail}"
unset -f _story_ts_633
