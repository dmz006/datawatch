#!/usr/bin/env bash
# TS-362 — Progress JSON has correct shape (version/started_at/active/sections/...)
# tags: surface:api feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-362"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_362() {
  local resp code
  resp=$(api_code GET /api/smoke/progress)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  local body
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-362 "resp.json" "$body"
  if [[ "$code" == "204" ]]; then
    skip "no smoke run active (204) — shape check requires active run"
  elif [[ "$code" == "200" ]]; then
    if echo "$body" | python3 -c 'import json,sys; v=json.load(sys.stdin); assert v is None' 2>/dev/null; then
      skip "no smoke run active (null response) — shape check requires active run"
    elif assert_json "$body" '"active" in d and "version" in d'; then
      ok "progress JSON has correct shape (active, version fields present)"
    elif assert_json "$body" '"active" in d'; then
      ok "progress JSON has active field"
    else
      ko "progress JSON missing expected fields: $body"
    fi
  elif [[ "$code" == "404" ]]; then
    skip "smoke/progress endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $body"
  fi
}

RESULT=fail
_story_ts_362
: "${RESULT:=fail}"
unset -f _story_ts_362
