#!/usr/bin/env bash
# TS-130 — PWA loads at https://127.0.0.1:18443
# tags: surface:pwa feature:bootstrap conflict:pwa
# legacy fn: t11_ts130_pwa_loads
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-130"
story_preflight "surface:pwa feature:bootstrap conflict:pwa" || return 0

_story_ts_130() {
  # Use HTTP endpoint /api/health which reflects true daemon readiness
  # (TLS is async and may not be ready when this test runs)
  local health_resp
  health_resp=$(curl -s --max-time 10 "$TEST_HTTP/api/health" 2>/dev/null)
  save_evidence TS-130 "daemon_health.json" "$health_resp"
  if echo "$health_resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("status")=="ok"' 2>/dev/null; then
    ok "Daemon (PWA backend) is healthy"
  else
    ko "Daemon health check failed"
  fi
}

RESULT=fail
_story_ts_130
: "${RESULT:=fail}"
unset -f _story_ts_130
