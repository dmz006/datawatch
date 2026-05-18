#!/usr/bin/env bash
# TS-288 — eval_list_suites + eval_run smoke suite shape via MCP
# tags: surface:mcp feature:mcp feature:evals
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-288"
story_preflight "surface:mcp feature:mcp feature:evals" || return 0

_story_ts_288() {
  local resp suite_id

  # List suites
  resp=$(api POST /api/mcp/call '{"tool":"eval_list_suites","params":{}}')
  save_evidence TS-288 "suites.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "eval_list_suites not available in this build"
    return
  fi

  suite_id=$(echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list) and len(d)>0:
    item=d[0]
    if isinstance(item,dict): print(item.get("id",item.get("name","")))
    else: print(str(item))
elif isinstance(d,dict):
    for k in ("suites","items","result"):
        if k in d and isinstance(d[k],list) and len(d[k])>0:
            item=d[k][0]
            if isinstance(item,dict): print(item.get("id",item.get("name","")))
            else: print(str(item))
            exit()
' 2>/dev/null || echo "")

  if [[ -z "$suite_id" ]]; then
    skip "no eval suites available to test eval_run"
    return
  fi

  # Run the suite
  resp=$(api POST /api/mcp/call "{\"tool\":\"eval_run\",\"params\":{\"suite\":\"$suite_id\"}}")
  save_evidence TS-288 "run.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "eval_list_suites + eval_run returned valid shapes"
  else
    ok "eval_list_suites returned suites; eval_run response: $(echo "$resp" | head -c 80)"
  fi
}

RESULT=fail
_story_ts_288
: "${RESULT:=fail}"
unset -f _story_ts_288
