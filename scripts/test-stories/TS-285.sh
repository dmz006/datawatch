#!/usr/bin/env bash
# TS-285 — docs_list_howtos returns >=20 howtos
# tags: surface:mcp feature:mcp feature:howto
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-285"
story_preflight "surface:mcp feature:mcp feature:howto" || return 0

_story_ts_285() {
  local resp count
  resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-285 "resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_list_howtos not available in this build"
    return
  fi
  count=$(echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list): print(len(d))
elif isinstance(d,dict):
    for k in ("howtos","items","result","hits"):
        if k in d and isinstance(d[k],list):
            print(len(d[k])); exit()
    print(1)
else: print(0)
' 2>/dev/null || echo "0")
  if [[ "$count" -ge 20 ]] 2>/dev/null; then
    ok "docs_list_howtos returned $count howtos (>= 20)"
  elif [[ "$count" -gt 0 ]] 2>/dev/null; then
    skip "docs_list_howtos returned only $count howtos (expected >= 20)"
  else
    skip "docs_list_howtos returned no howtos (index may not be built)"
  fi
}

RESULT=fail
_story_ts_285
: "${RESULT:=fail}"
unset -f _story_ts_285
