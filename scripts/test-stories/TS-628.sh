#!/usr/bin/env bash
# TS-628 — POST /api/alert-rules creates rule and GET confirms it
# tags: surface:rest feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-628"
story_preflight "surface:rest feature:alert-rules" || return 0

_story_ts_628() {
  local rule_name="e2e-alert-rule-ts628"
  local payload resp code get_resp

  # Create
  payload="{\"name\":\"$rule_name\",\"description\":\"e2e test\",\"condition\":{\"metric\":\"cpu_pct\",\"operator\":\">\",\"threshold\":95},\"action\":{\"kind\":\"alert\"},\"window_seconds\":60,\"cooldown_seconds\":300,\"enabled\":true}"
  resp=$(api_code POST /api/alert-rules "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  resp=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-628 "create.json" "$resp"

  if [[ "$code" == "503" || "$code" == "501" ]]; then
    skip "alert-rules endpoint not available (HTTP $code)"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "POST /api/alert-rules expected 2xx, got $code: $(echo "$resp" | head -c 200)"
    return
  fi

  # Confirm with GET
  get_resp=$(api GET "/api/alert-rules/$rule_name")
  save_evidence TS-628 "get.json" "$get_resp"

  # Cleanup
  api DELETE "/api/alert-rules/$rule_name" >/dev/null 2>&1 || true

  if assert_json "$get_resp" 'd.get("name") == "'"$rule_name"'"'; then
    ok "POST /api/alert-rules creates rule and GET confirms it"
  else
    ko "GET did not confirm created rule: $(echo "$get_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_628
: "${RESULT:=fail}"
unset -f _story_ts_628
