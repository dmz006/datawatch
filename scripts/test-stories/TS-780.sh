#!/usr/bin/env bash
# TS-780 — Automaton directory scope: set_dirs round-trip and validation
# (write_dirs / read_dirs persisted, relative paths rejected).
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-780"
story_preflight "surface:api feature:automata" || return 0

_story_ts_780() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local resp prd_id
  resp=$(api POST /api/autonomous/prds '{"spec":"scope round-trip","project_dir":"/tmp","effort":"low"}')
  save_evidence "TS-780" "0_create_prd.json" "$resp"
  prd_id=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then skip "could not create PRD"; return; fi
  add_cleanup "automaton" "$prd_id"

  resp=$(api POST "/api/autonomous/prds/$prd_id/set_dirs" '{"write_dirs":["/tmp/ts780-out"],"read_dirs":["/tmp/ts780-ref"]}')
  save_evidence "TS-780" "1_set_dirs.json" "$resp"
  resp=$(api GET "/api/autonomous/prds/$prd_id")
  if echo "$resp" | python3 -c "import json,sys; p=json.load(sys.stdin); sys.exit(0 if p.get('write_dirs')==['/tmp/ts780-out'] and p.get('read_dirs')==['/tmp/ts780-ref'] else 1)"; then
    ok "set_dirs persisted write_dirs and read_dirs"
  else
    ko "set_dirs not persisted: $(echo "$resp" | head -c 200)"
  fi

  local code
  code=$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$TEST_BASE/api/autonomous/prds/$prd_id/set_dirs" \
    -H 'Content-Type: application/json' ${TEST_TOKEN:+-H "Authorization: Bearer $TEST_TOKEN"} \
    -d '{"write_dirs":["relative/dir"]}')
  if [[ "$code" == "400" ]]; then ok "relative directory rejected with 400"; else ko "expected 400 for relative dir, got $code"; fi
}

RESULT=fail
_story_ts_780
: "${RESULT:=fail}"
unset -f _story_ts_780
