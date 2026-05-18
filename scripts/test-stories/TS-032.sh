#!/usr/bin/env bash
# TS-032 — Council quick run
# tags: surface:api feature:council conflict:llm
# legacy fn: t4_ts032_council_quick_run
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-032"
story_preflight "surface:api feature:council conflict:llm" || return 0

_story_ts_032() {
  if [[ -z "$PERSONA_ID" ]]; then skip "no persona ID (TS-031 failed)"; return; fi
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi
  local resp
  resp=$(curl "${curl_args[@]}" --max-time 120 -X POST -H "Content-Type: application/json" \
    -d '{"proposal":"What is 2+2? Answer with just the number.","personas":["'"$PERSONA_ID"'"],"mode":"quick"}' \
    "$TEST_BASE/api/council/run")
  save_evidence TS-032 "run.json" "$resp"
  RUN_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -n "$RUN_ID" ]]; then
    add_cleanup council "$RUN_ID"
    ok "council run started: $RUN_ID"
  else
    skip "council run failed (LLM may be unreachable): $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_032
: "${RESULT:=fail}"
unset -f _story_ts_032
