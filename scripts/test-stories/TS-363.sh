#!/usr/bin/env bash
# TS-363 — After smoke completes, active=false in progress JSON
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-363"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_363() {
  local resp code body
  resp=$(api_code GET /api/smoke/progress)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-363 "resp.json" "$body"
  if [[ "$code" == "204" ]]; then
    skip "no smoke run active or no completed run (204) — requires completed smoke run"
  elif [[ "$code" == "200" ]]; then
    if echo "$body" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v is None' 2>/dev/null; then
      skip "no smoke run data (null response) — requires completed smoke run"
      return
    fi
    local active
    active=$(echo "$body" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("active",""))' 2>/dev/null || echo "")
    if [[ "$active" == "False" || "$active" == "false" ]]; then
      ok "active=false after smoke completion"
    elif [[ "$active" == "True" || "$active" == "true" ]]; then
      skip "smoke run appears to be active; test requires completed run"
    else
      ko "active field unexpected value '$active': $body"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_363
: "${RESULT:=fail}"
unset -f _story_ts_363
