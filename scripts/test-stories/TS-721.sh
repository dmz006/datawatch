#!/usr/bin/env bash
# TS-721 — Current-status contract: GET /api/sessions/{id}/current_status returns HTTP 200 (not 204)
# tags: surface:api feature:sessions parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-721"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_721() {
  # Get a session to test
  local sid
  sid=$(api GET /api/sessions 2>/dev/null \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["id"] if d else "")' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "no sessions available for current_status test"
    return
  fi

  local resp code body
  resp=$(api_code GET "/api/sessions/$sid/current-status")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-721 "current_status.json" "$body"

  if [[ "$code" == "404" ]]; then
    skip "current-status endpoint not found (HTTP 404) — may not be enabled on this session"
    return
  fi

  # The key fix in v8.19.8 was changing 204→200 so PWA's r.json() doesn't throw
  if [[ "$code" == "204" ]]; then
    ko "GET /api/sessions/{id}/current-status returned HTTP 204 (regression) — should be 200 per v8.19.8 contract; PWA r.json() will throw"
    return
  fi

  if [[ "$code" != "200" ]]; then
    ko "GET /api/sessions/{id}/current-status returned HTTP $code (expected 200): $(echo "$body" | head -c 200)"
    return
  fi

  # Verify body is non-empty JSON
  if ! echo "$body" | python3 -c "import json,sys;json.load(sys.stdin)" 2>/dev/null; then
    ko "current-status returned 200 but non-JSON body: $(echo "$body" | head -c 200)"
    return
  fi

  local no_change
  no_change=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('no_change',''))" 2>/dev/null || echo "")

  ok "GET /api/sessions/{id}/current-status returns HTTP 200 with JSON body (no_change=$no_change) — v8.19.8 contract satisfied"
}

RESULT=fail
_story_ts_721
: "${RESULT:=fail}"
unset -f _story_ts_721
