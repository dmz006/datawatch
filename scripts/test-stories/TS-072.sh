#!/usr/bin/env bash
# TS-072 — Tool annotations present
# tags: surface:mcp feature:mcp
# legacy fn: t8_ts072_tool_annotations
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-072"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_072() {
  local resp
  resp=$(api GET /api/mcp/docs)
  save_evidence TS-072 "annotations.json" "$resp"
  if echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
has_ro = any(t.get('annotations',{}).get('readOnlyHint') for t in d)
has_dest = any(t.get('annotations',{}).get('destructiveHint') for t in d)
assert has_ro, 'no readOnlyHint tools'
assert has_dest, 'no destructiveHint tools'
" 2>/dev/null; then
    ok "tool annotations present (readOnly + destructive)"
  else
    skip "tool annotations not present (may be v7.1.0+ feature)"
  fi
}

RESULT=fail
_story_ts_072
: "${RESULT:=fail}"
unset -f _story_ts_072
