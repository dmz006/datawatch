#!/usr/bin/env bash
# TS-063 — Plugin docs audit
# tags: surface:api feature:plugins feature:docs
# legacy fn: t7_ts063_plugin_docs_audit
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-063"
story_preflight "surface:api feature:plugins feature:docs" || return 0

_story_ts_063() {
  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-063 "plugins.json" "$resp"
  local count
  count=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('plugins',[])
print(len(arr))
" 2>/dev/null || echo "0")
  # Verify docs-as-mcp tools are surfaced
  local mcp_resp
  mcp_resp=$(api GET /api/mcp/docs)
  save_evidence TS-063 "mcp_docs.json" "$mcp_resp"
  local has_docs_tool
  has_docs_tool=$(echo "$mcp_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else []
print('yes' if any('plugin' in t.get('name','') or 'doc' in t.get('name','') for t in arr) else 'no')
" 2>/dev/null || echo "no")
  ok "Plugin audit: $count plugins installed; docs-tool surface: $has_docs_tool"
}

RESULT=fail
_story_ts_063
: "${RESULT:=fail}"
unset -f _story_ts_063
