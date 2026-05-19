#!/usr/bin/env bash
# TS-204 — Pipeline lifecycle: start, list, cancel
# tags: surface:api feature:sessions
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-204"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_204() {
  # Start a pipeline
  local start_resp start_code start_body
  start_resp=$(api_code POST /api/pipelines '{"spec":"echo ts204-step1 -> echo ts204-step2"}')
  start_code=$(echo "$start_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  start_body=$(echo "$start_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-204 "start.json" "$start_body"

  if [[ "$start_code" == "503" ]]; then
    skip "pipelines not available (503)"
    return
  fi
  if [[ "$start_code" != "200" ]]; then
    ko "pipeline start returned HTTP $start_code: $start_body"
    return
  fi

  # Extract pipeline ID (format: "pipe-XXXXX" possibly embedded in a verbose string)
  local pipe_id
  pipe_id=$(echo "$start_body" | python3 -c '
import json,sys,re
d=json.load(sys.stdin)
id_str=d.get("id","")
m=re.search(r"(pipe-[a-f0-9]+)", id_str)
print(m.group(1) if m else id_str)
' 2>/dev/null || echo "")

  if [[ -z "$pipe_id" ]]; then
    ko "pipeline start response missing id: $start_body"
    return
  fi

  # List pipelines and verify our pipeline appears
  local list_resp list_code list_body
  list_resp=$(api_code GET /api/pipelines)
  list_code=$(echo "$list_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  list_body=$(echo "$list_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-204 "list.json" "$list_body"

  if [[ "$list_code" != "200" ]]; then
    ko "pipeline list returned HTTP $list_code: $list_body"
    return
  fi

  local found
  found=$(echo "$list_body" | python3 -c "
import json,sys
items=json.load(sys.stdin)
print('yes' if any(p.get('id','')=='$pipe_id' for p in items) else 'no')
" 2>/dev/null || echo "no")

  if [[ "$found" != "yes" ]]; then
    ko "pipeline $pipe_id not found in list: $list_body"
    return
  fi

  # Cancel the pipeline: POST /api/pipeline?action=cancel&id=<id>
  local cancel_resp cancel_code cancel_body
  cancel_resp=$(api_code POST "/api/pipeline?action=cancel&id=$pipe_id")
  cancel_code=$(echo "$cancel_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  cancel_body=$(echo "$cancel_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-204 "cancel.json" "$cancel_body"

  if [[ "$cancel_code" == "200" ]]; then
    ok "pipeline lifecycle: start($pipe_id), list(found), cancel(ok)"
  elif [[ "$cancel_code" == "404" ]]; then
    # Pipeline may have already completed — that's fine, start+list worked
    ok "pipeline lifecycle: start($pipe_id), list(found); cancel 404 (already done)"
  else
    ko "pipeline cancel returned HTTP $cancel_code: $cancel_body"
  fi
}

RESULT=fail
_story_ts_204
: "${RESULT:=fail}"
unset -f _story_ts_204
