#!/usr/bin/env bash
# TS-631 — POST /api/alert-rules/{name}/enable and disable return ok
# tags: surface:rest feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-631"
story_preflight "surface:rest feature:alert-rules" || return 0

_story_ts_631() {
  local rule_name="e2e-alert-rule-ts631"
  local payload resp code

  # Create a rule first
  payload="{\"name\":\"$rule_name\",\"condition\":{\"metric\":\"cpu_pct\",\"operator\":\">\",\"threshold\":95},\"action\":{\"kind\":\"alert\"},\"window_seconds\":60,\"cooldown_seconds\":300,\"enabled\":true}"
  resp=$(api_code POST /api/alert-rules "$payload")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  if [[ "$code" == "503" || "$code" == "501" ]]; then
    skip "alert-rules endpoint not available (HTTP $code)"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    skip "could not create test rule for enable/disable (HTTP $code)"
    return
  fi

  # Disable
  resp=$(api_code POST "/api/alert-rules/$rule_name/disable")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-631 "disable.json" "$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')"
  if [[ ! "$code" =~ ^2 ]]; then
    api DELETE "/api/alert-rules/$rule_name" >/dev/null 2>&1 || true
    ko "POST /api/alert-rules/$rule_name/disable expected 2xx, got $code"
    return
  fi

  # Enable
  resp=$(api_code POST "/api/alert-rules/$rule_name/enable")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-631 "enable.json" "$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')"

  # Cleanup
  api DELETE "/api/alert-rules/$rule_name" >/dev/null 2>&1 || true

  if [[ "$code" =~ ^2 ]]; then
    ok "POST /api/alert-rules/{name}/enable and disable return ok"
  else
    ko "POST /api/alert-rules/$rule_name/enable expected 2xx, got $code"
  fi
}

RESULT=fail
_story_ts_631
: "${RESULT:=fail}"
unset -f _story_ts_631
