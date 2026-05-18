#!/usr/bin/env bash
# TS-620 — v8.0 smoke — daemon health returns 200 with status:ok
# tags: surface:api feature:routing group:routing-v8 parallel:ok blocking
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-620"
story_preflight "surface:api feature:routing group:routing-v8 parallel:ok blocking" || return 0

_story_ts_620() {
  local health code
  code=$(curl "${curl_args[@]}" -o /tmp/r620_health.json -w "%{http_code}" "$TEST_BASE/api/health" 2>/dev/null || echo "000")
  health=$(cat /tmp/r620_health.json 2>/dev/null || echo "{}")
  save_evidence TS-620 "health.json" "$health"

  if [[ "$code" != "200" ]]; then
    ko "GET /api/health expected 200, got $code"
    return
  fi

  if assert_json "$health" 'd.get("status")=="ok"'; then
    ok "daemon health returns 200 with status:ok"
  else
    ko "daemon health missing status:ok: $health"
  fi
}

RESULT=fail
_story_ts_620
: "${RESULT:=fail}"
unset -f _story_ts_620
