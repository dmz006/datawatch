#!/usr/bin/env bash
# TS-544 — POST /api/council/personas creates persona with required fields
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-544"
story_preflight "surface:api feature:council" || return 0

_story_ts_544() {
  local resp code
  resp=$(api_code POST /api/council/personas \
    "{\"name\":\"test-persona-$$\",\"description\":\"test persona for TS-544\",\"system_prompt\":\"You are helpful.\"}")
  save_evidence TS-544 "create.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" == "200" || "$code" == "201" ]]; then
    local persona_id
    persona_id=$(echo "$resp" | python3 -c 'import json,sys;body="".join(l for l in sys.stdin.read().split("\n") if not l.startswith("__HTTP_CODE"));d=json.loads(body);print(d.get("id",""))' 2>/dev/null || echo "")
    [[ -n "$persona_id" ]] && add_cleanup persona "$persona_id"
    ok "POST /api/council/personas returned $code"
  elif [[ "$code" == "404" ]]; then
    skip "council/personas endpoint not available (404)"
  else
    ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_544
: "${RESULT:=fail}"
unset -f _story_ts_544
