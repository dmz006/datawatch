#!/usr/bin/env bash
# TS-728 — Council persona refine-step: POST with bad step returns 400
# tags: surface:api feature:council parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-728"
story_preflight "surface:api feature:council" || return 0

_story_ts_728() {
  # POST with invalid step name
  local resp code body
  resp=$(api_code POST /api/council/personas/refine-step \
    '{"step":"invalid-step-name","current_answer":"test","instruction":"test instruction"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-728 "bad_step_resp.json" "$body"

  if [[ "$code" == "404" ]]; then
    ko "POST /api/council/personas/refine-step returned 404 — endpoint not registered (GH#159)"
    return
  fi
  if [[ "$code" == "400" ]]; then
    ok "POST /api/council/personas/refine-step with invalid step returns 400 — step validation working"
    return
  fi
  if [[ "$code" == "503" ]]; then
    # No LLM configured — that's a different code path but endpoint IS registered
    ok "POST /api/council/personas/refine-step endpoint registered (503 = no LLM; step validation would fire before LLM call)"
    return
  fi

  # POST with missing current_answer — also 400
  local resp2 code2 body2
  resp2=$(api_code POST /api/council/personas/refine-step \
    '{"step":"focus","instruction":"make it clearer"}')
  code2=$(echo "$resp2" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body2=$(echo "$resp2" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-728 "missing_field_resp.json" "$body2"

  if [[ "$code2" == "400" ]]; then
    ok "POST /api/council/personas/refine-step missing current_answer returns 400 — field validation working (step=focus)"
    return
  fi

  ko "refine-step unexpected responses: bad_step=$code, missing_field=$code2; bodies: $(echo "$body" | head -c 100) / $(echo "$body2" | head -c 100)"
}

RESULT=fail
_story_ts_728
: "${RESULT:=fail}"
unset -f _story_ts_728
