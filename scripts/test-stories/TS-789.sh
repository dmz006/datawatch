#!/usr/bin/env bash
# TS-789 — BL406 Phase 2: parity-inheritance rule (ships default-on,
# scan.DefaultConfig()) fires a real finding when a story's description
# drops a surface the parent plan's own "## Parity surface" section
# named. Structural edits (add_story) are rejected by the executor
# unless the PRD is already in needs_review/revisions_asked/cancelled
# (found live while writing this story — a fresh PRD starts in
# "draft", which locks add_story/add_task) — the only REST-reachable
# way there is a real decompose, so this needs conflict:llm. Once in
# needs_review, add_story gives deterministic, controlled content for
# the actual parity assertion rather than depending on whatever the
# live LLM happened to produce.
# tags: surface:api feature:automata conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-789"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_789() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi

  local spec='TS-789 parity-inheritance fixture plan.

## Parity surface

| Capability | REST | MCP |
|---|---|---|
| widget | yes | yes |
'
  local atm atm_id
  atm=$(api POST /api/autonomous/prds "$(python3 -c 'import json,sys; print(json.dumps({"spec": sys.argv[1], "project_dir": "/tmp", "effort": "low", "backend": "ollama"}))' "$spec")")
  save_evidence TS-789 "0_create.json" "$atm"
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$atm_id" ]]; then
    ko "could not create fixture automaton: $(echo "$atm" | head -c 200)"
    return
  fi
  add_cleanup automaton "$atm_id"

  # Real decompose, purely to cross draft -> needs_review (the only
  # REST-reachable transition that unlocks add_story). Don't care what
  # it produces; the actual assertion uses our own story below.
  local decomp_resp
  decomp_resp=$(curl "${curl_args[@]}" --max-time 300 -X POST "$TEST_BASE/api/autonomous/prds/$atm_id/decompose" -w "\n__HTTP_CODE_%{http_code}__")
  local status=""
  for i in $(seq 1 60); do
    status=$(api GET "/api/autonomous/prds/$atm_id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    [[ "$status" == "needs_review" || "$status" == "approved" ]] && break
    sleep 2
  done
  if [[ "$status" != "needs_review" ]]; then
    skip "could not reach needs_review via decompose (status=$status, LLM may be unreachable in test env)"
    return
  fi

  # Our own controlled story, deliberately omitting "MCP".
  local story_resp
  story_resp=$(api POST "/api/autonomous/prds/$atm_id/add_story" '{"title":"TS-789 story missing MCP","description":"Implements the widget over REST only."}')
  save_evidence TS-789 "1_add_story.json" "$story_resp"
  if ! assert_json "$story_resp" '"stories" in d'; then
    skip "add_story failed even after reaching needs_review: $(echo "$story_resp" | head -c 200)"
    return
  fi

  local scan_resp found
  scan_resp=$(api POST "/api/autonomous/prds/$atm_id/scan" '{}')
  save_evidence TS-789 "2_scan.json" "$scan_resp"
  found=$(echo "$scan_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);fs=d.get("findings",[]);print(1 if any(f.get("rule_id")=="parity-inheritance" and "MCP" in f.get("message","") and "TS-789 story missing MCP" in f.get("message","") for f in fs) else 0)' 2>/dev/null || echo 0)

  if [[ "$found" == "1" ]]; then
    ok "default parity-inheritance rule flags our controlled story for dropping the MCP surface (BL406 Phase 2, v9.0.13)"
  else
    ko "expected a parity-inheritance finding naming our story's missing MCP surface, got none: $(echo "$scan_resp" | head -c 500)"
  fi
}

RESULT=fail
_story_ts_789
: "${RESULT:=fail}"
unset -f _story_ts_789
