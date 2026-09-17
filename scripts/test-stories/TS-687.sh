#!/usr/bin/env bash
# TS-687 — POST /api/autonomous/prds/{id} with memory_seed.from_prds accepted
# tags: surface:api feature:memory feature:automata group:memory-lifecycle-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-687"
story_preflight "surface:api feature:memory group:memory-lifecycle-v9" || return 0

_story_ts_687() {
  local prd_id src_prd_id code resp
  m_enabled=$(api GET /api/memory/stats | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$m_enabled" != "yes" ]] && { skip "memory not enabled"; return; }

  # Create source PRD.
  src_prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-687 source","project_dir":"/tmp","backend":"opencode"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$src_prd_id" ]]; then
    skip "could not create source PRD"
    return
  fi

  # Create child PRD with from_prds cross-seeding config.
  prd_id=$(api POST /api/autonomous/prds \
    "{\"spec\":\"TS-687 child\",\"project_dir\":\"/tmp\",\"backend\":\"opencode\",\"memory_seed\":{\"enabled\":true,\"from_prds\":[\"$src_prd_id\"]}}" \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")

  # Cleanup.
  api DELETE "/api/autonomous/prds/$src_prd_id" >/dev/null 2>&1 || true
  [[ -n "$prd_id" ]] && api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true

  if [[ -z "$prd_id" ]]; then
    ko "could not create PRD with from_prds memory_seed config"
    return
  fi
  ok "PRD with from_prds cross-seeding config accepted (id=$prd_id)"
}

RESULT=fail
_story_ts_687
: "${RESULT:=fail}"
unset -f _story_ts_687
