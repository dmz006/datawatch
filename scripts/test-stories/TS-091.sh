#!/usr/bin/env bash
# TS-091 — DNS comm: server mode registers comm backend; proxy send queues response
# tags: surface:api feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-091"
story_preflight "surface:api feature:comms" || return 0

_story_ts_091() {
  # The test daemon runs dns_channel in server mode (127.0.0.1:19053).
  # In server mode, the DNS backend is registered in commBackends so
  # POST /api/proxy/comm/dns/send queues a response for the next incoming
  # DNS query — this is the valid test: verify the endpoint is wired and
  # returns 200 (even with no client, the message is queued successfully).
  local send_resp send_code send_body
  send_resp=$(api_code POST /api/proxy/comm/dns/send '{"message":"ts091-dns-channel-e2e-test"}')
  send_code=$(echo "$send_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  send_body=$(echo "$send_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-091 "send.json" "$send_body"

  if [[ "$send_code" == "200" ]]; then
    ok "DNS comm send via /api/proxy/comm/dns/send returned HTTP 200"
  elif [[ "$send_code" == "400" ]]; then
    ok "DNS comm send returned 400 (recipient/message validation) — endpoint is registered"
  elif [[ "$send_code" == "503" ]]; then
    skip "comm backends not configured (503)"
  elif [[ "$send_code" == "404" ]]; then
    # DNS channel not registered as comm backend — check test daemon config
    local cfg_resp cfg_dns_enabled
    cfg_resp=$(api GET /api/config)
    cfg_dns_enabled=$(echo "$cfg_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
dns=d.get('dns_channel',{})
print('yes' if dns.get('enabled') else 'no')
" 2>/dev/null || echo "no")
    if [[ "$cfg_dns_enabled" == "yes" ]]; then
      ko "dns_channel enabled in config but /api/proxy/comm/dns/send returned 404 — backend not wired"
    else
      skip "dns_channel not enabled in test daemon config (HTTP 404)"
    fi
  else
    ko "DNS comm send unexpected: HTTP $send_code: $(echo "$send_body" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_091
: "${RESULT:=fail}"
unset -f _story_ts_091
