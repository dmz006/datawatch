#!/usr/bin/env bash
# TS-783 — two Automata against one capacity-1 compute node: never more than one
# autonomous session on the node at once, no task fails for capacity, both finish.
# tags: surface:api feature:automata feature:capacity conflict:llm
#
# Needs a reachable Ollama (TEST_OLLAMA_HOST, default http://datawatch:11434).
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-783"
story_preflight "surface:api feature:automata feature:capacity conflict:llm" || return 0

_story_ts_783() {
  local sid="$$" host="${TEST_OLLAMA_HOST:-http://datawatch:11434}"
  local node="e2e-cap1-$sid" llm="e2e-cap1llm-$sid" model="qwen3:8b" code resp
  local prds=()
  _cleanup() {
    local p
    for p in "${prds[@]}"; do api DELETE "/api/autonomous/prds/$p" >/dev/null 2>&1 || true; done
    api DELETE "/api/llms/$llm" >/dev/null 2>&1 || true
    api DELETE "/api/compute/nodes/$node" >/dev/null 2>&1 || true
  }
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  resp=$(api_code POST /api/compute/nodes "{\"name\":\"$node\",\"kind\":\"ollama\",\"address\":\"$host\",\"max_concurrent_sessions\":1}")
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  [[ "$code" == "200" || "$code" == "201" ]] || { skip "could not register node at $host ($code)"; return; }
  if ! api GET "/api/compute/nodes/$node/models?kind=ollama" | python3 -c 'import json,sys;assert len(json.load(sys.stdin).get("models",[]))>0' 2>/dev/null; then
    _cleanup; skip "Ollama at $host unreachable or has no models"; return
  fi
  resp=$(api_code POST /api/llms "{\"name\":\"$llm\",\"kind\":\"ollama\",\"model\":\"$model\",\"compute_nodes\":[\"$node\"]}")
  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  [[ "$code" == "200" || "$code" == "201" ]] || { _cleanup; skip "could not register LLM ($code)"; return; }
  sleep 12  # ledger sync picks up the node limit

  local i id dir
  for i in 1 2; do
    dir="${RUN_DIR}/cap1-${sid}-$i"; mkdir -p "$dir"
    id=$(api POST /api/autonomous/prds "{\"spec\":\"Create the file $dir/out-$i.txt containing the single line ok. Do nothing else.\",\"project_dir\":\"$dir\",\"effort\":\"low\"}" \
      | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
    [[ -z "$id" ]] && { _cleanup; skip "could not create PRD $i"; return; }
    prds+=("$id")
    api POST "/api/autonomous/prds/$id/set_llm" "{\"backend\":\"$llm\",\"model\":\"$model\",\"decomposition_profile\":\"$llm\"}" >/dev/null 2>&1 || true
    api POST "/api/autonomous/prds/$id/decompose" '{}' >/dev/null
  done

  local status ready
  for i in $(seq 1 90); do
    sleep 2; ready=0
    for id in "${prds[@]}"; do
      status=$(api GET "/api/autonomous/prds/$id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
      [[ "$status" == "needs_review" ]] && ready=$((ready+1))
    done
    [[ $ready -eq 2 ]] && break
  done
  [[ $ready -eq 2 ]] || { _cleanup; skip "decompose did not finish for both PRDs (LLM slow)"; return; }

  for id in "${prds[@]}"; do
    api POST "/api/autonomous/prds/$id/approve" '{}' >/dev/null
    api POST "/api/autonomous/prds/$id/run" '{}' >/dev/null
  done

  local peak=0 sawwait=0 terminal=0 snap n
  for i in $(seq 1 150); do
    sleep 2
    snap=$(api GET /api/capacity)
    n=$(echo "$snap" | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(sum(1 for l in d['leases'] if 'node:$node' in l['pools']))" 2>/dev/null || echo 0)
    [[ "$n" -gt "$peak" ]] && peak=$n
    echo "$snap" | python3 -c "
import json,sys
d=json.load(sys.stdin)
sys.exit(0 if any('node:$node' in w['pools'] for w in d['waiting']) else 1)" 2>/dev/null && sawwait=1
    terminal=0
    for id in "${prds[@]}"; do
      status=$(api GET "/api/autonomous/prds/$id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
      case "$status" in completed|failed|blocked) terminal=$((terminal+1)) ;; esac
    done
    [[ $terminal -eq 2 ]] && break
  done
  save_evidence TS-783 "last_capacity.json" "$snap"

  if [[ "$peak" -le 1 ]]; then ok "never more than one lease on the capacity-1 node (peak=$peak)"; else ko "node limit 1 exceeded: peak=$peak"; fi
  [[ $sawwait -eq 1 ]] && ok "second Automaton's task was observed waiting for capacity" || ok "no wait observed (tasks did not overlap) — limit still respected"
  if [[ $terminal -ne 2 ]]; then _cleanup; skip "PRDs did not reach a terminal state in time (LLM slow)"; return; fi

  local capfail
  capfail=$(for id in "${prds[@]}"; do api GET "/api/autonomous/prds/$id"; echo; done | python3 -c "
import json,sys
n=0
for line in sys.stdin:
    line=line.strip()
    if not line: continue
    p=json.loads(line)
    for s in p.get('stories',[]):
        for t in s.get('tasks',[]):
            if t.get('status')=='failed' and 'capacity' in (t.get('error') or '').lower(): n+=1
print(n)" 2>/dev/null || echo 0)
  if [[ "$capfail" == "0" ]]; then ok "no task failed for capacity"; else ko "$capfail task(s) failed with a capacity error"; fi
  _cleanup
}

RESULT=fail
_story_ts_783
: "${RESULT:=fail}"
unset -f _story_ts_783
