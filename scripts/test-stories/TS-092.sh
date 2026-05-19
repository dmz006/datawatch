#!/usr/bin/env bash
# TS-092 — GET /api/stats DNS entry enabled:true
# tags: surface:comms feature:comms
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-092"
story_preflight "surface:comms feature:comms" || return 0

_story_ts_092() {
  # The test daemon is configured with dns_channel.enabled=true (server mode, 127.0.0.1:19053)
  # Verify the MCP dns_channel_config_get tool or GET /api/config reports it enabled
  local resp
  resp=$(api GET /api/stats)
  save_evidence TS-092 "stats.json" "$resp"

  local dns_enabled
  dns_enabled=$(echo "$resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
comm = d.get('comm_stats', d.get('comms', {}))
if isinstance(comm, dict):
    dns = comm.get('dns', comm.get('DNS', {}))
    if isinstance(dns, dict):
        print('yes' if dns.get('enabled') else 'no')
    else:
        print('no')
else:
    print('no')
" 2>/dev/null || echo "no")

  if [[ "$dns_enabled" == "yes" ]]; then
    ok "GET /api/stats: dns comm_stats entry has enabled:true"
    return
  fi

  # Fallback: check via config get
  local cfg_resp
  cfg_resp=$(api GET /api/config)
  save_evidence TS-092 "config.json" "$cfg_resp"
  local cfg_dns_enabled
  cfg_dns_enabled=$(echo "$cfg_resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
dns = d.get('dns_channel', {})
print('yes' if dns.get('enabled') else 'no')
" 2>/dev/null || echo "no")

  if [[ "$cfg_dns_enabled" == "yes" ]]; then
    ok "dns_channel.enabled=true confirmed via /api/config"
  else
    ko "DNS channel not enabled in test daemon (check testdata/datawatch.yaml dns_channel block)"
  fi
}

RESULT=fail
_story_ts_092
: "${RESULT:=fail}"
unset -f _story_ts_092
