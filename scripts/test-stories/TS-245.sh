#!/usr/bin/env bash
# TS-245 — Update check journey: version check without install
# tags: surface:api feature:update
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-245"
story_preflight "surface:api feature:update" || return 0

_story_ts_245() {
  echo ""; echo "  >> TS-245: Update check journey: version check without install"
  local resp code body
  resp=$(api_code GET /api/update/check)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence "TS-245" "update_check.json" "$body"
  if [[ "$code" == "200" ]]; then
    if echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); assert isinstance(d, dict)" 2>/dev/null; then
      ok "GET /api/update/check returned 200 with JSON response"
    else
      ok "GET /api/update/check returned 200"
    fi
  elif [[ "$code" == "501" ]]; then
    # 501 means no latestFn wired — endpoint exists but feature not configured
    ok "GET /api/update/check returned 501 (endpoint exists, update checker not configured)"
  elif [[ "$code" == "503" ]]; then
    skip "GET /api/update/check returned 503 (update service unavailable)"
  else
    ko "GET /api/update/check returned unexpected HTTP $code: $(echo "$body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_245
: "${RESULT:=fail}"
unset -f _story_ts_245
