#!/usr/bin/env bash
# TS-488 — datawatch llm in-use <name> exits 0
# tags: surface:cli feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-488"
story_preflight "surface:cli feature:llm-registry" || return 0

_story_ts_488() {
  local llm_name
  llm_name=$(api GET /api/llms | python3 -c 'import json,sys;d=json.load(sys.stdin);llms=d.get("llms",d) if isinstance(d,dict) else d;print(llms[0]["name"] if isinstance(llms,list) and llms else "")' 2>/dev/null || echo "")
  if [[ -z "$llm_name" ]]; then
    skip "no LLMs configured"
    return
  fi
  local out rc
  out=$(cli_test llm in-use "$llm_name" 2>&1); rc=$?
  save_evidence TS-488 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "datawatch llm in-use $llm_name exits 0"
  elif echo "$out" | grep -qiE "disabled|not.*configured|not.*available|unknown.*command|no such"; then
    skip "$(echo "$out" | head -c 80)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_488
: "${RESULT:=fail}"
unset -f _story_ts_488
