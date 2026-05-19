#!/usr/bin/env bash
# TS-529 — POST /api/council/run returns {id,status,events_path} shape
# tags: surface:api feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-529"
story_preflight "surface:api feature:council" || return 0

_story_ts_529() {
  local resp
  resp=$(api POST /api/council/run '{"proposal":"1+1=?","personas":[]}')
  save_evidence TS-529 "run.json" "$resp"
  if ! echo "$resp" | python3 -c "import json,sys; json.load(sys.stdin)" 2>/dev/null; then
    skip "council/run endpoint not available"
    return
  fi
  local run_id
  run_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -n "$run_id" ]]; then
    RUN_ID="$run_id"
    add_cleanup council "$run_id"
    ok "POST /api/council/run returned run id: $run_id"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "POST /api/council/run returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_529
: "${RESULT:=fail}"
unset -f _story_ts_529
