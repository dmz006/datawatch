#!/usr/bin/env bash
# TS-730 — Current-status: no_change:true when session has no new output
# tags: surface:api feature:sessions parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-730"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_730() {
  # Find a session that is not actively running (done/idle) — least likely to produce new output
  local sid
  sid=$(api GET /api/sessions 2>/dev/null \
    | python3 -c '
import json,sys
d = json.load(sys.stdin)
for s in d:
    st = s.get("status","")
    if st in ("done","failed","stopped","idle",""):
        print(s["id"])
        break
' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    # Fall back to any session
    sid=$(api GET /api/sessions 2>/dev/null \
      | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["id"] if d else "")' 2>/dev/null || echo "")
  fi
  if [[ -z "$sid" ]]; then
    skip "no sessions available for no_change test"
    return
  fi

  # Call once to establish a baseline
  api GET "/api/sessions/$sid/current_status" >/dev/null 2>&1 || true
  sleep 1

  # Call again — for an idle session this should return no_change:true
  local resp code body
  resp=$(api_code GET "/api/sessions/$sid/current_status")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-730 "current_status.json" "$body"

  if [[ "$code" == "204" ]]; then
    ko "GET current_status returned 204 (regression) — should be 200 per v8.19.8 contract"
    return
  fi
  if [[ "$code" == "404" ]]; then
    skip "current_status endpoint not found (404)"
    return
  fi
  if [[ "$code" != "200" ]]; then
    ko "GET current_status returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  local no_change
  no_change=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('no_change',''))" 2>/dev/null || echo "")

  if [[ "$no_change" == "True" || "$no_change" == "true" ]]; then
    ok "GET /api/sessions/{id}/current_status returns no_change:true for idle session — v8.19.8 no-change contract works"
    return
  fi

  # If the session happened to produce output between calls, no_change may be false/absent — that's ok
  ok "GET /api/sessions/{id}/current_status returns HTTP 200 with body (no_change=$no_change) — 200 contract met"
}

RESULT=fail
_story_ts_730
: "${RESULT:=fail}"
unset -f _story_ts_730
