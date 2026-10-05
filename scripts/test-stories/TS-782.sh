#!/usr/bin/env bash
# TS-782 — capacity admission surface: GET /api/capacity shape, config round-trip for
# autonomous.capacity_* and session.reserved_interactive, PRD priority, node/LLM limits persist.
# tags: surface:api feature:automata feature:capacity feature:config conflict:llm
#
# conflict:llm (reused, not about LLM contention per se): this test
# temporarily sets the GLOBAL autonomous.capacity_enabled=false and
# capacity_gpu_util_pct=91, which affects admission for every
# concurrently-running autonomous PRD, not just this test's own. Found
# alongside the TS-026 per_story_approval race (2026-10-05) during the
# same investigation — same class of bug (global shared daemon config
# mutated without exclusivity against other autonomous-PRD-lifecycle
# tests). conflict:llm keeps this out of the same wall-clock window as
# those tests.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-782"
story_preflight "surface:api feature:automata feature:capacity feature:config" || return 0

_story_ts_782() {
  local resp before sid="$$"

  resp=$(api GET /api/capacity)
  save_evidence TS-782 "0_capacity.json" "$resp"
  if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert isinstance(d['pools'],list) and isinstance(d['leases'],list) and isinstance(d['waiting'],list)" 2>/dev/null; then
    ok "GET /api/capacity returns pools, leases and waiting arrays"
  else
    ko "GET /api/capacity shape wrong: $(echo "$resp" | head -c 200)"
  fi

  before=$(api GET /api/config)
  api PUT /api/config '{"autonomous.capacity_enabled": false, "autonomous.capacity_wait_timeout_seconds": 777, "autonomous.capacity_gpu_util_pct": 91, "session.reserved_interactive": 2}' >/dev/null
  resp=$(api GET /api/config)
  if echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
a=d['autonomous']; s=d['session']
sys.exit(0 if a['capacity_enabled'] is False and a['capacity_wait_timeout_seconds']==777 and a['capacity_gpu_util_pct']==91 and s['reserved_interactive']==2 else 1)"; then
    ok "capacity config keys round-trip via PUT then GET /api/config"
  else
    ko "capacity config did not round-trip: $(echo "$resp" | head -c 300)"
  fi
  api PUT /api/config '{"autonomous.capacity_enabled": true, "autonomous.capacity_wait_timeout_seconds": 0, "autonomous.capacity_gpu_util_pct": 0}' >/dev/null

  local node="e2e-capnode-$sid"
  api POST "/api/compute/nodes?probe=skip" "{\"name\":\"$node\",\"kind\":\"ollama\",\"address\":\"http://127.0.0.1:1\",\"max_concurrent_sessions\":1}" >/dev/null
  add_cleanup "compute-node" "$node"
  resp=$(api GET "/api/compute/nodes/$node")
  if echo "$resp" | python3 -c "import json,sys; sys.exit(0 if json.load(sys.stdin).get('max_concurrent_sessions')==1 else 1)"; then
    ok "compute node max_concurrent_sessions persisted"
  else
    ko "max_concurrent_sessions not persisted: $(echo "$resp" | head -c 200)"
  fi
  sleep 12  # ledger sync loop (10 s) picks up the new node limit
  resp=$(api GET /api/capacity)
  if echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
sys.exit(0 if any(p['name']=='node:$node' and p['limit']==1 for p in d['pools']) else 1)"; then
    ok "capacity ledger picked up node limit as pool node:$node"
  else
    ko "pool node:$node with limit 1 not present in /api/capacity"
  fi
  api DELETE "/api/compute/nodes/$node" >/dev/null 2>&1 || true

  local prd_id
  prd_id=$(api POST /api/autonomous/prds '{"spec":"capacity priority round-trip","project_dir":"/tmp","effort":"low"}' | python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
  if [[ -n "$prd_id" ]]; then
    add_cleanup "automaton" "$prd_id"
    api POST "/api/autonomous/prds/$prd_id/set_priority" '{"priority": 7}' >/dev/null
    if api GET "/api/autonomous/prds/$prd_id" | python3 -c "import json,sys; sys.exit(0 if json.load(sys.stdin).get('priority')==7 else 1)"; then
      ok "set_priority persisted"
    else
      ko "set_priority not persisted"
    fi
  else
    skip "could not create PRD for priority check"
  fi
}

RESULT=fail
_story_ts_782
: "${RESULT:=fail}"
unset -f _story_ts_782
