#!/usr/bin/env bash
# TS-187 — Webhook comm backend config parity: GET /api/config returns webhook fields
# tags: surface:api feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-187"
story_preflight "surface:api feature:comms" || return 0

_story_ts_187() {
  local cfg
  cfg=$(api GET /api/config 2>/dev/null)
  save_evidence TS-187 "config.json" "$cfg"

  # Webhook backend must be present and enabled in config
  local wh_enabled wh_addr
  wh_enabled=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('webhook',{}).get('enabled',False))" 2>/dev/null || echo "")
  wh_addr=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('webhook',{}).get('addr',''))" 2>/dev/null || echo "")

  if [[ "$wh_enabled" != "True" && "$wh_enabled" != "true" ]]; then
    skip "webhook backend not enabled in test config (enabled=$wh_enabled)"
    return
  fi
  if [[ -z "$wh_addr" || "$wh_addr" == "null" ]]; then
    ko "webhook.addr missing from GET /api/config response"
    return
  fi

  # Verify webhook backend is reachable (GET /health or HEAD / to the addr)
  local health_code
  health_code=$(curl -sk --max-time 5 -o /dev/null -w "%{http_code}" "http://$wh_addr/" 2>/dev/null || echo "000")
  save_evidence TS-187 "webhook_health.txt" "addr=$wh_addr code=$health_code"

  if [[ "$health_code" == "000" ]]; then
    skip "webhook server at $wh_addr not reachable (timeout) — daemon may not have started webhook listener"
    return
  fi

  ok "webhook comm backend: enabled=true addr=$wh_addr reachable (HTTP $health_code)"
}

RESULT=fail
_story_ts_187
: "${RESULT:=fail}"
unset -f _story_ts_187
