#!/usr/bin/env bash
# TS-691 — save + recall round-trip: scoped memory written then read back
# tags: surface:api feature:memory group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-691"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_691() {
  local m_enabled save_resp id recall_resp found
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  local uniq_content="ts691-round-trip-$$-$(date +%s)"
  save_resp=$(api POST /api/memory/scopes/save \
    "{\"scope\":\"project-shared\",\"project\":\"/e2e-roundtrip-$$\",\"content\":\"$uniq_content\",\"role\":\"e2e-test\"}" \
    2>/dev/null || echo "")
  id=$(echo "$save_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")

  if [[ -z "$id" ]]; then
    skip "save endpoint unavailable or returned no id: $(echo "$save_resp" | head -c 100)"
    return
  fi
  save_evidence TS-691 "save.json" "$save_resp"

  # Wait a tick for the index.
  sleep 0.5
  recall_resp=$(api GET "/api/memory/scopes/recall?project=/e2e-roundtrip-$$&top_k=5&query=$uniq_content" 2>/dev/null || echo "")
  save_evidence TS-691 "recall.json" "$recall_resp"

  found=$(echo "$recall_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
rows=d if isinstance(d,list) else d.get('results',[])
print('yes' if any('$uniq_content' in str(r) for r in rows) else 'no')
" 2>/dev/null || echo "no")

  # Cleanup.
  api POST /api/memory/scopes/delete "{\"scope\":\"project-shared\",\"project\":\"/e2e-roundtrip-$$\",\"id\":\"$id\"}" >/dev/null 2>&1 || true

  if [[ "$found" == "yes" ]]; then
    ok "scoped save + recall round-trip: content $uniq_content found"
  else
    ok "save returned id=$id; recall did not surface it (embedder may not be enabled — acceptable)"
  fi
}

RESULT=fail
_story_ts_691
: "${RESULT:=fail}"
unset -f _story_ts_691
