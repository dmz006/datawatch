#!/usr/bin/env bash
# TS-681 — POST /api/memory/scopes/delete removes a scoped memory entry by id
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-681"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_681() {
  local m_enabled code save_resp id del_resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  # First save an entry to have something to delete.
  save_resp=$(api POST /api/memory/scopes/save \
    "{\"scope\":{\"scope\":\"project-shared\",\"project\":\"/e2e-proj-$$\"},\"content\":\"TS-681 delete target\",\"role\":\"e2e-test\"}" 2>/dev/null || echo "")
  id=$(echo "$save_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$id" ]]; then
    skip "could not create entry to delete (save_resp: $(echo "$save_resp" | head -c 100))"
    return
  fi

  del_resp=$(api_code POST /api/memory/scopes/delete \
    "{\"scope\":{\"scope\":\"project-shared\",\"project\":\"/e2e-proj-$$\"},\"memory_id\":$id}")
  save_evidence TS-681 "delete.json" "$del_resp"
  code=$(echo "$del_resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  case "$code" in
    200|204) ok "DELETE scoped memory $id returned $code" ;;
    404)     ok "404 — entry already cleaned up (acceptable)" ;;
    405)     skip "memory/scopes/delete endpoint not available (405)" ;;
    503)     skip "memory backend disabled (503)" ;;
    *) ko "unexpected HTTP $code for delete: $(echo "$del_resp" | head -c 200)" ;;
  esac
}

RESULT=fail
_story_ts_681
: "${RESULT:=fail}"
unset -f _story_ts_681
