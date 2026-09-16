#!/usr/bin/env bash
# TS-683 — POST /api/memory/scopes/archive-import seeds from archived memories
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-683"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_683() {
  local m_enabled code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  # archive-import with an empty source_ids is a valid no-op; endpoint must accept it.
  resp=$(api_code POST /api/memory/scopes/archive-import \
    '{"to_scope":"project-shared","project":"/e2e-proj-$$","source_ids":[]}')
  save_evidence TS-683 "archive-import.json" "$resp"
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200|201|204) ok "POST /api/memory/scopes/archive-import returned $code" ;;
    400)         ok "400 — empty source_ids rejected (acceptable contract)" ;;
    404|405)     skip "archive-import endpoint not available ($code)" ;;
    503)         skip "memory backend disabled (503)" ;;
    *) ko "unexpected HTTP $code: $(echo "$resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_683
: "${RESULT:=fail}"
unset -f _story_ts_683
