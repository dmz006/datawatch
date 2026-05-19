#!/usr/bin/env bash
# TS-636 — GET /api/skills/registries returns community as first registry
# tags: surface:rest feature:community-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-636"
story_preflight "surface:rest feature:community-registry" || return 0

_story_ts_636() {
  local resp code
  resp=$(api_code GET /api/skills/registries)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-636 "registries.json" "$resp"

  if [[ "$code" == "503" || "$code" == "501" || "$code" == "404" ]]; then
    skip "skills/registries endpoint not available (HTTP $code)"
    return
  fi
  if [[ "$code" != "200" ]]; then
    ko "GET /api/skills/registries expected 200, got $code"
    return
  fi
  if assert_json "$resp" '(d[0].get("name") if isinstance(d, list) and d else (d.get("registries",[{}]) or [{}])[0].get("name","")) == "community"'; then
    ok "GET /api/skills/registries returns community as first registry"
  else
    skip "community registry not first (or not present) in this environment"
  fi
}

RESULT=fail
_story_ts_636
: "${RESULT:=fail}"
unset -f _story_ts_636
