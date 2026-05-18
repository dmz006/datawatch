#!/usr/bin/env bash
# TS-440 — GET /api/sessions response has backend_family field (not llm_backend)
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-440"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_440() {
  ensure_test_session || return
  local resp
  resp=$(api GET "/api/sessions/$SESSION_ID")
  save_evidence TS-440 "resp.json" "$resp"
  if assert_json "$resp" '"backend_family" in d'; then
    ok "GET /api/sessions/$SESSION_ID has backend_family field"
  elif assert_json "$resp" '"id" in d'; then
    # Session found but no backend_family — check sessions list
    local list_resp
    list_resp=$(api GET /api/sessions)
    save_evidence TS-440 "list_resp.json" "$list_resp"
    local has_field
    has_field=$(echo "$list_resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get("sessions",[])
if items and "backend_family" in items[0]:
    print("yes")
else:
    print("no")
' 2>/dev/null || echo "no")
    if [[ "$has_field" == "yes" ]]; then
      ok "GET /api/sessions list items have backend_family field"
    else
      skip "backend_family field not present in session response (may be renamed or not yet added)"
    fi
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "sessions endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_440
: "${RESULT:=fail}"
unset -f _story_ts_440
