#!/usr/bin/env bash
# TS-726 — INFO endpoint: GET /api/info returns whisper_configured field
# tags: surface:api feature:voice parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-726"
story_preflight "surface:api feature:voice" || return 0

_story_ts_726() {
  local resp code body
  resp=$(api_code GET /api/info)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-726 "info.json" "$body"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/info returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # whisper_configured field added in GH#157
  local has_field
  has_field=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'whisper_configured' in d else 'no')" 2>/dev/null || echo "no")
  if [[ "$has_field" != "yes" ]]; then
    ko "GET /api/info missing 'whisper_configured' field (GH#157): $(echo "$body" | head -c 200)"
    return
  fi

  local val
  val=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('whisper_configured',''))" 2>/dev/null || echo "")
  ok "GET /api/info includes whisper_configured=$val (GH#157 field present)"
}

RESULT=fail
_story_ts_726
: "${RESULT:=fail}"
unset -f _story_ts_726
