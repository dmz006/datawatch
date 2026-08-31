#!/usr/bin/env bash
# TS-658 — GET /api/config vision section includes all 7 fields
# tags: surface:api feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-658"
story_preflight "surface:api feature:vision group:vision parallel:ok" || return 0

_story_ts_658() {
  local resp
  resp=$(api GET /api/config)
  save_evidence TS-658 "config.json" "$resp"

  for field in enabled backend endpoint api_key model default_prompt max_image_bytes; do
    if ! echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'vision' in d and '$field' in d['vision']" 2>/dev/null; then
      ko "GET /api/config vision.$field missing"
      return
    fi
  done
  ok "GET /api/config vision section includes all 7 fields"
}

RESULT=fail
_story_ts_658
: "${RESULT:=fail}"
unset -f _story_ts_658
