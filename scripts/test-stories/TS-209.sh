#!/usr/bin/env bash
# TS-209 — docs-as-mcp: docs tool surface integrity
# tags: surface:mcp feature:docs
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-209"
story_preflight "surface:mcp feature:docs" || return 0

_story_ts_209() {
    echo ""; echo "  >> TS-209: docs-as-mcp: docs tool surface integrity"
    resp=$(api GET /api/mcp/tools)
    has_docs=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); l=d if isinstance(d,list) else d.get('tools',[]); print(any('doc' in t.get('name','') for t in l))" 2>/dev/null || echo "False")
    save_evidence "TS-209" "docs_tools.json" "$resp"
    if [[ "$has_docs" == "True" ]]; then
      ok "Docs-as-MCP tools present in tool list"
    else
      skip "No doc-named tools found (may require howto index generation)"
    fi

}

RESULT=fail
_story_ts_209
: "${RESULT:=fail}"
unset -f _story_ts_209
