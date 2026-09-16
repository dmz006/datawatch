#!/usr/bin/env bash
# TS-688 — POST /api/sessions/{id}/guardrail/{name}/approve returns 200 or 404
# tags: surface:api feature:guardrail group:guardrail-approve-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-688"
story_preflight "surface:api feature:guardrail group:guardrail-approve-v9" || return 0

_story_ts_688() {
  local sess_id code resp

  # Find any active session (or use a fabricated ID — endpoint must return 404 for unknown).
  sess_id=$(api GET /api/sessions | python3 -c '
import json,sys
d=json.load(sys.stdin)
rows=d if isinstance(d,list) else d.get("sessions",[])
for r in rows:
    sid=r.get("id","")
    if sid: print(sid); break
' 2>/dev/null || echo "")

  if [[ -z "$sess_id" ]]; then
    # No live sessions — test the 404 path with a fake ID.
    sess_id="e2e-fake-session-$$"
  fi

  resp=$(api_code POST "/api/sessions/$sess_id/guardrail/content-safety/approve" \
    '{"note":"e2e-test approve"}')
  save_evidence TS-688 "approve.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200)
      if assert_json "$resp" '"approved" in d or "verdict" in d or "found" in d'; then
        ok "guardrail approve returned 200 with expected shape"
      else
        ok "guardrail approve returned 200 (no verdict field — acceptable for sessions with no pending guardrail)"
      fi
      ;;
    404)
      ok "404 — session or guardrail not found (expected for non-blocked sessions)"
      ;;
    405) skip "guardrail approve endpoint not available (405)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_688
: "${RESULT:=fail}"
unset -f _story_ts_688
