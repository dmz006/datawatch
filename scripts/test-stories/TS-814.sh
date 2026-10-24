#!/usr/bin/env bash
# TS-814 — Automata progress metrics read the same on REST, MCP, CLI and comm
# tags: surface:api feature:autonomous-progress
#
# Runs against the sandbox daemon only (TEST_BASE is https on the sandbox TLS
# port). It creates its own records inline: one tag, two Automata (one set to
# kind bug with set_kind, the other left without a kind so it derives to
# feature), and scopes every read to that tag so other stories' records cannot
# change the counts. No session is started and nothing is approved or run, so
# the lead-time and median fields have no samples and must be null, not zero.
# Everything it creates is deleted at the end.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-814"
story_preflight "surface:api feature:autonomous-progress" || return 0

_ts814_fail=0
_ts814_check() { # <label> <exit status of the check>
  if [[ "$2" == "0" ]]; then
    echo "  ok    [TS-814] $1"
  else
    echo "  FAIL  [TS-814] $1"
    _ts814_fail=$((_ts814_fail + 1))
  fi
}

# _ts814_scoped_metrics <json> — the tag-scoped object holds one feature and
# one bug Automaton, nothing else, and no measured wall time or lead time.
_ts814_scoped_metrics() {
  echo "$1" | python3 -c '
import json, sys
d = json.load(sys.stdin)
g = d["groups"]
assert g["feature"]["prds"] == 1, g["feature"]
assert g["bug"]["prds"] == 1, g["bug"]
assert g["other"]["prds"] == 0, g["other"]
assert d["totals"]["prds"] == 2, d["totals"]
lt = d["lead_time"]
for k in ("created_to_approved_median_hours", "created_to_last_done_median_hours"):
    assert k in lt and lt[k] is None, (k, lt.get(k))
for k in ("feature", "bug"):
    assert "median_prd_wall_hours" in g[k] and g[k]["median_prd_wall_hours"] is None, g[k]
' 2>/dev/null
}

_ts814_json_field() { # <json> <python expression over d>
  echo "$1" | python3 -c "import json,sys; d=json.load(sys.stdin); print($2)" 2>/dev/null
}

_story_ts_811() {
  local sfx tag_id id_feature id_bug resp out

  if [[ "$TEST_BASE" != https://* ]]; then
    ko "TEST_BASE must be the https sandbox address, got: $TEST_BASE"
    return
  fi
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then
    ko "autonomous mode is off in the sandbox config; the story needs it on to create records"
    return
  fi

  sfx="$$-$(date +%s)"

  # --- fixtures -------------------------------------------------------------
  resp=$(api POST /api/autonomous/tags "{\"name\":\"progress-e2e-$sfx\"}")
  tag_id=$(_ts814_json_field "$resp" 'd.get("id","")')
  if [[ -z "$tag_id" ]]; then
    ko "could not create the scoping tag: $(echo "$resp" | head -c 200)"
    return
  fi

  resp=$(api POST /api/autonomous/prds "{\"spec\":\"Progress metrics record one $sfx: write a short hello note\",\"project_dir\":\"/tmp\",\"effort\":\"low\"}")
  id_feature=$(_ts814_json_field "$resp" 'd.get("id","")')
  [[ -n "$id_feature" ]] && add_cleanup automaton "$id_feature"
  resp=$(api POST /api/autonomous/prds "{\"spec\":\"Progress metrics record two $sfx: write a short hello note\",\"project_dir\":\"/tmp\",\"effort\":\"low\"}")
  id_bug=$(_ts814_json_field "$resp" 'd.get("id","")')
  [[ -n "$id_bug" ]] && add_cleanup automaton "$id_bug"
  if [[ -z "$id_feature" || -z "$id_bug" ]]; then
    ko "could not create the two Automata: $(echo "$resp" | head -c 200)"
    _ts814_cleanup "$tag_id" "$id_feature" "$id_bug"
    return
  fi

  resp=$(api POST "/api/autonomous/prds/$id_bug/set_kind" '{"kind":"bug"}')
  _ts814_check "set_kind bug is stored" "$(assert_json "$resp" 'd.get("kind") == "bug"'; echo $?)"
  api POST "/api/autonomous/prds/$id_feature/set_tags" "{\"tag_ids\":[\"$tag_id\"]}" >/dev/null
  api POST "/api/autonomous/prds/$id_bug/set_tags" "{\"tag_ids\":[\"$tag_id\"]}" >/dev/null

  # --- REST -----------------------------------------------------------------
  out=$(api GET "/api/autonomous/progress?tag_id=$tag_id")
  save_evidence TS-814 "rest-scoped.json" "$out"
  _ts814_scoped_metrics "$out"
  _ts814_check "REST scoped read: one feature, one bug, wall time and lead time null" "$?"

  out=$(api GET /api/autonomous/progress)
  save_evidence TS-814 "rest-all.json" "$out"
  assert_json "$out" 'd["groups"]["feature"]["prds"] >= 1 and d["groups"]["bug"]["prds"] >= 1 and d["totals"]["prds"] >= 2'
  _ts814_check "REST unscoped read counts at least the two new Automata" "$?"

  # --- MCP ------------------------------------------------------------------
  out=$(api_mcp autonomous_progress "{\"tag_id\":\"$tag_id\"}")
  save_evidence TS-814 "mcp-scoped.json" "$out"
  _ts814_scoped_metrics "$out"
  _ts814_check "MCP autonomous_progress returns the same scoped object" "$?"

  # --- CLI ------------------------------------------------------------------
  if [[ ! -x "$TEST_BINARY" ]]; then
    _ts814_check "CLI binary present at $TEST_BINARY" 1
  else
    out=$(cli_test autonomous progress --json --tag "$tag_id" 2>/dev/null)
    save_evidence TS-814 "cli-scoped.json" "$out"
    _ts814_scoped_metrics "$out"
    _ts814_check "CLI autonomous progress --json returns the same scoped object" "$?"
  fi

  # --- comm channel ---------------------------------------------------------
  resp=$(api POST /api/test/message '{"text":"progress"}')
  save_evidence TS-814 "comm.json" "$resp"
  assert_json "$resp" 'd.get("count", 0) >= 1 and "Progress since" in "\n".join(d["responses"])'
  _ts814_check "comm verb progress replies with the progress table" "$?"

  resp=$(api POST /api/test/message "{\"text\":\"progress tag=$tag_id\"}")
  save_evidence TS-814 "comm-scoped.json" "$resp"
  echo "$resp" | python3 -c '
import json, re, sys
t = "\n".join(json.load(sys.stdin)["responses"])
for row in (r"^feature +1 ", r"^bug +1 ", r"^total +2 "):
    assert re.search(row, t, re.M), row
' 2>/dev/null
  _ts814_check "comm verb progress tag=<id> shows one feature, one bug, two in total" "$?"

  # --- trend ----------------------------------------------------------------
  # The start-up snapshot is written shortly after the daemon starts, so poll.
  local tries=0 points=0
  while (( tries < 15 )); do
    out=$(api GET /api/autonomous/progress/trend)
    points=$(_ts814_json_field "$out" 'len(d["points"])')
    [[ "${points:-0}" -ge 1 ]] && break
    tries=$((tries + 1)); sleep 3
  done
  save_evidence TS-814 "trend.json" "$out"
  [[ "${points:-0}" -ge 1 ]]
  _ts814_check "trend returns at least the start-up snapshot (points=${points:-0})" "$?"

  # --- bad input ------------------------------------------------------------
  resp=$(api_code POST "/api/autonomous/prds/$id_bug/set_kind" '{"kind":"nonsense"}')
  [[ "$resp" == *__HTTP_CODE_400__* ]]
  _ts814_check "set_kind with an unknown kind is refused with 400" "$?"
  resp=$(api GET "/api/autonomous/prds/$id_bug")
  assert_json "$resp" 'd.get("kind") == "bug"'
  _ts814_check "the refused set_kind left the stored kind alone" "$?"
  resp=$(api_code POST /api/autonomous/prds "{\"spec\":\"Progress metrics refused $sfx\",\"project_dir\":\"/tmp\",\"kind\":\"nonsense\"}")
  [[ "$resp" == *__HTTP_CODE_400__* ]]
  _ts814_check "create with an unknown kind is refused with 400" "$?"
  resp=$(api_code GET "/api/autonomous/progress?since=not-a-date")
  [[ "$resp" == *__HTTP_CODE_400__* ]]
  _ts814_check "a malformed since date is refused with 400" "$?"

  # --- cleanup --------------------------------------------------------------
  _ts814_cleanup "$tag_id" "$id_feature" "$id_bug"

  if (( _ts814_fail == 0 )); then
    ok "progress metrics agree on REST, MCP, CLI and comm; trend, bad kind and cleanup checked"
  else
    ko "$_ts814_fail check(s) failed"
  fi
}

# _ts814_cleanup <tag id> <automaton id>... — delete what the story created.
_ts814_cleanup() {
  local tag_id="$1" id code
  shift
  for id in "$@"; do
    [[ -z "$id" ]] && continue
    code=$(api_code DELETE "/api/autonomous/prds/$id?hard=true" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
    _ts814_check "cleanup: Automaton $id deleted" "$([[ "$code" =~ ^2 ]]; echo $?)"
  done
  code=$(api_code DELETE "/api/autonomous/tags/$tag_id" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  _ts814_check "cleanup: scoping tag deleted" "$([[ "$code" =~ ^2 ]]; echo $?)"
}

RESULT=fail
_story_ts_811
: "${RESULT:=fail}"
unset -f _story_ts_811 _ts814_cleanup _ts814_check _ts814_scoped_metrics _ts814_json_field
unset _ts814_fail
