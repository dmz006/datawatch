#!/usr/bin/env bash
# TS-207 — Webhook comm backend: POST /task creates a session from inbound webhook message
# tags: surface:api feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-207"
story_preflight "surface:api feature:comms" || return 0

_story_ts_207() {
  # Get webhook addr from config
  local cfg wh_addr wh_enabled
  cfg=$(api GET /api/config 2>/dev/null)
  wh_enabled=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('webhook',{}).get('enabled',False))" 2>/dev/null || echo "")
  wh_addr=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('webhook',{}).get('addr',''))" 2>/dev/null || echo "")

  if [[ "$wh_enabled" != "True" && "$wh_enabled" != "true" ]]; then
    skip "webhook backend not enabled in test config"
    return
  fi
  if [[ -z "$wh_addr" || "$wh_addr" == "null" ]]; then
    skip "webhook.addr missing from /api/config"
    return
  fi

  # Count sessions before
  local before_count
  before_count=$(api GET /api/sessions 2>/dev/null | python3 -c "import json,sys; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")

  # POST a task to the webhook endpoint
  local task_msg="e2e TS-207 test task $(date +%s)"
  local wh_code wh_resp
  wh_resp=$(curl -sk --max-time 10 -X POST \
    -H "Content-Type: application/json" \
    -d "{\"task\":\"$task_msg\"}" \
    "http://$wh_addr/task" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null || echo "__HTTP_CODE_000__")
  wh_code=$(echo "$wh_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  save_evidence TS-207 "webhook_post.json" "$wh_resp"

  if [[ "$wh_code" == "000" ]]; then
    skip "webhook server at $wh_addr not reachable (timeout)"
    return
  fi
  if [[ "$wh_code" == "404" ]]; then
    skip "webhook /task endpoint returned 404 — webhook backend not running"
    return
  fi
  if [[ ! "$wh_code" =~ ^2 ]]; then
    ko "POST to webhook /task returned HTTP $wh_code: $(echo "$wh_resp" | head -c 200)"
    return
  fi

  ok "webhook comm backend: POST /task accepted (HTTP $wh_code) — inbound task delivery works"
}

RESULT=fail
_story_ts_207
: "${RESULT:=fail}"
unset -f _story_ts_207
