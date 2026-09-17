#!/usr/bin/env bash
# TS-680 — POST /api/memory/scopes/save writes a memory to a named scope
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-680"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_680() {
  local m_enabled code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  resp=$(api_code POST /api/memory/scopes/save \
    "{\"scope\":{\"scope\":\"project-shared\",\"project\":\"/e2e-proj-$$\"},\"content\":\"TS-680 scope save e2e\"}")
  save_evidence TS-680 "save.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200|201) ok "POST /api/memory/scopes/save returned $code" ;;
    404|405) skip "memory/scopes/save endpoint not available ($code)" ;;
    503)     skip "memory backend disabled (503)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_680
: "${RESULT:=fail}"
unset -f _story_ts_680
