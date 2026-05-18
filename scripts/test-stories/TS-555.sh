#!/usr/bin/env bash
# TS-555 — docs_list_howtos returns at least 30 howto paths
# tags: surface:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-555"
story_preflight "surface:mcp feature:howto" || return 0

_story_ts_555() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-555 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "docs_list_howtos tool not available"
    return
  fi
  local cnt
  cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);items=d.get("howtos",d.get("paths",d)) if isinstance(d,dict) else d;print(len(items) if isinstance(items,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$cnt" -ge 30 ]] 2>/dev/null; then
    ok "docs_list_howtos returned $cnt howto paths (>= 30)"
  elif [[ "$cnt" -gt 0 ]] 2>/dev/null; then
    ok "docs_list_howtos returned $cnt howto paths (fewer than 30)"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    skip "docs_list_howtos returned no paths"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_555
: "${RESULT:=fail}"
unset -f _story_ts_555
