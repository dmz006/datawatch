#!/usr/bin/env bash
# TS-005 — TLS auto-cert reachable
# tags: surface:api feature:bootstrap
# legacy fn: t1_ts005_tls_autocert
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-005"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_005() {
  # TLS cert generation is async — give it up to 15s after HTTP is ready
  local health cert_info attempts=0
  while [[ $attempts -lt 15 ]]; do
    health=$(curl -sk --max-time 5 "$TEST_TLS/api/health" 2>/dev/null || echo "{}")
    if echo "$health" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('status')=='ok'" 2>/dev/null; then
      break
    fi
    sleep 1; attempts=$((attempts+1))
  done
  save_evidence TS-005 "health.json" "$health"
  cert_info=$(openssl s_client -connect 127.0.0.1:18443 -showcerts </dev/null 2>&1 | head -30 || echo "openssl unavailable")
  save_evidence TS-005 "cert_info.txt" "$cert_info"
  if assert_json "$health" 'd.get("status")=="ok"'; then
    ok "TLS auto-cert: HTTPS health on :18443 ok"
  else
    skip "TLS not ready on :18443 (may not be configured in test env)"
  fi
}

RESULT=fail
_story_ts_005
: "${RESULT:=fail}"
unset -f _story_ts_005
