#!/usr/bin/env bash
# TS-659 — PUT /api/config vision.* fields round-trip via GET
# tags: surface:api feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-659"
story_preflight "surface:api feature:vision group:vision parallel:ok" || return 0

_story_ts_659() {
  local resp code

  resp=$(api_code PUT /api/config '{"vision.enabled":true,"vision.backend":"ollama","vision.model":"llava"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-659 "put.json" "$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')"

  if [[ ! "$code" =~ ^2 ]]; then
    ko "PUT /api/config vision fields expected 2xx, got $code"
    return
  fi

  local get_resp
  get_resp=$(api GET /api/config)
  save_evidence TS-659 "get.json" "$get_resp"

  if ! echo "$get_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); v=d['vision']; assert v['backend']=='ollama' and v['model']=='llava'" 2>/dev/null; then
    ko "GET /api/config did not reflect vision.backend=ollama, vision.model=llava"
    return
  fi
  ok "PUT /api/config vision fields round-trip correctly"
}

RESULT=fail
_story_ts_659
: "${RESULT:=fail}"
unset -f _story_ts_659
