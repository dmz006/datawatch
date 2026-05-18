#!/usr/bin/env bash
# TS-279 — cost_rates + cost_summary shape via MCP
# tags: surface:mcp feature:mcp feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-279"
story_preflight "surface:mcp feature:mcp feature:config" || return 0

_story_ts_279() {
  local resp

  # cost_rates
  resp=$(api POST /api/mcp/call '{"tool":"cost_rates","params":{}}')
  save_evidence TS-279 "rates.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "cost_rates not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "cost_rates unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # cost_summary
  resp=$(api POST /api/mcp/call '{"tool":"cost_summary","params":{}}')
  save_evidence TS-279 "summary.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "cost_rates + cost_summary both return valid shapes"
  else
    ko "cost_summary unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_279
: "${RESULT:=fail}"
unset -f _story_ts_279
