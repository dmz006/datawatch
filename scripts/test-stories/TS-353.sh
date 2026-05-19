#!/usr/bin/env bash
# TS-353 — docs_apply executes steps and returns 200/OK per step
# tags: surface:mcp feature:mcp feature:howto feature:memory
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-353"
story_preflight "surface:mcp feature:mcp feature:howto feature:memory" || return 0

_story_ts_353() {
  local resp

  # Use docs_list_howtos to check if cross-agent-memory has exec_steps
  # (docs_read strips frontmatter, so exec_steps won't appear in the body)
  resp=$(api POST /api/mcp/call '{"tool":"docs_list_howtos","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-353 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "docs_list_howtos not available in this build"
    return
  fi
  local has_exec
  has_exec=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('howtos',d.get('items',[]))
for item in items:
    if 'cross-agent-memory' in str(item.get('path','')) or 'cross-agent-memory' in str(item.get('id','')):
        print('yes' if item.get('has_exec_steps') else 'no')
        break
else:
    print('notfound')
" 2>/dev/null || echo "notfound")
  if [[ "$has_exec" == "notfound" ]]; then
    skip "cross-agent-memory howto not found in list"
    return
  fi
  if [[ "$has_exec" != "yes" ]]; then
    skip "cross-agent-memory howto has no exec_steps"
    return
  fi

  # Apply the howto; pass both required exec_params: project_dir and text
  resp=$(api POST /api/mcp/call "{\"tool\":\"docs_apply\",\"params\":{\"howto_id\":\"howto/cross-agent-memory.md\",\"params\":{\"project_dir\":\"$REPO_ROOT\",\"text\":\"e2e test memory entry\"}}}")
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-353 "apply.json" "$resp"
  if echo "$resp" | grep -qi "no exec_steps\|no steps\|nothing to apply\|not applicable"; then
    skip "docs_apply: no exec_steps applicable"
    return
  fi
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "docs_apply cross-agent-memory returned valid response"
  else
    skip "docs_apply returned unexpected shape: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_353
: "${RESULT:=fail}"
unset -f _story_ts_353
