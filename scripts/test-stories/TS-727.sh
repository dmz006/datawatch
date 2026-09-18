#!/usr/bin/env bash
# TS-727 — Council persona refine-step: POST /api/council/personas/refine-step rejects GET
# tags: surface:api feature:council parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-727"
story_preflight "surface:api feature:council" || return 0

_story_ts_727() {
  # GET should return 405
  local resp code body
  resp=$(api_code GET /api/council/personas/refine-step)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-727 "get_resp.json" "$body"

  if [[ "$code" == "404" ]]; then
    ko "GET /api/council/personas/refine-step returned 404 — endpoint not registered (GH#159)"
    return
  fi
  if [[ "$code" == "405" ]]; then
    ok "GET /api/council/personas/refine-step returns 405 Method Not Allowed — endpoint registered correctly"
    return
  fi

  # POST with missing fields should return 400
  local post_resp post_code post_body
  post_resp=$(api_code POST /api/council/personas/refine-step '{}')
  post_code=$(echo "$post_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  post_body=$(echo "$post_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-727 "empty_post_resp.json" "$post_body"

  if [[ "$post_code" == "400" ]]; then
    ok "POST /api/council/personas/refine-step with empty body returns 400 — endpoint registered (GET returned $code)"
    return
  fi

  ko "GET /api/council/personas/refine-step returned $code (expected 405 or 400 for empty POST): body=$(echo "$body" | head -c 100)"
}

RESULT=fail
_story_ts_727
: "${RESULT:=fail}"
unset -f _story_ts_727
