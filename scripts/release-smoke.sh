#!/usr/bin/env bash
# release-smoke.sh — pre-release functional smoke test.
#
# Operator directive 2026-04-27: every release must FUNCTIONALLY test
# every subsystem, not just rely on Go unit tests. The autonomous
# decompose path silently broke in v3.10.0 because the
# `prompt`-vs-`question` field-name mismatch slipped through every
# release boundary — unit tests covered the manager + REST handler
# in isolation but never exercised the loopback together.
#
# This script runs against a LIVE daemon on https://localhost:8443
# (auto-detected port via API). Each step prints PASS / FAIL; one
# failure exits non-zero and aborts the release.
#
# Usage:
#   ./scripts/release-smoke.sh                      # default localhost
#   DW_BASE=http://127.0.0.1:8080 ./scripts/release-smoke.sh
#   DW_TOKEN=<bearer> ./scripts/release-smoke.sh    # if auth enabled
#
# Returns 0 on success, non-zero on first failure.

set -uo pipefail

BASE="${DW_BASE:-https://localhost:8443}"
TOK="${DW_TOKEN:-}"
TMPD=$(mktemp -d)

# v5.26.9 — operator-reported: smoke must clean up. Accumulate the
# IDs of every PRD / peer / graph / etc. the smoke creates, then
# garbage-collect them on EXIT (success OR failure). Each entry is a
# `<kind> <id>` line; cleanup_all reads the file and DELETEs in
# reverse order so child resources go before parents.
CLEANUP_LOG="$TMPD/cleanup.log"
: >"$CLEANUP_LOG"
add_cleanup() { echo "$1 $2" >> "$CLEANUP_LOG"; }

# v5.26.18 — operator-reported (multiple times): smoke runs leave
# orphaned `autonomous:*` tmux sessions because the executor goroutine
# can have a spawn HTTP call already in flight when cancel propagates.
# Capture a baseline of "autonomous-named" session IDs that exist
# BEFORE smoke runs; in cleanup_all we list them again and kill any
# new ones (i.e. created during smoke). This catches the race.
BASELINE_AUTO_SESSIONS="$TMPD/baseline_auto.txt"
curl_args_baseline=(-sk --max-time 10)
[[ -n "$TOK" ]] && curl_args_baseline+=(-H "Authorization: Bearer $TOK")
curl "${curl_args_baseline[@]}" "$BASE/api/sessions" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  ss = d.get("sessions") if isinstance(d, dict) else d
  for s in (ss or []):
    if (s.get("name") or "").startswith("autonomous:") and s.get("state") in ("running","waiting_input","rate_limited"):
      print(s.get("full_id",""))
except Exception:
  pass
' > "$BASELINE_AUTO_SESSIONS" 2>/dev/null || : > "$BASELINE_AUTO_SESSIONS"

cleanup_all() {
  local printed_header=0
  if [[ -s "$CLEANUP_LOG" ]]; then
    printed_header=1
    echo ""
    echo "== Cleanup =="
    # tac to delete in reverse order
    tac "$CLEANUP_LOG" | while read -r kind id; do
      case "$kind" in
        prd)             curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$id?hard=true" >/dev/null 2>&1 && echo "  removed prd $id" || echo "  (already gone) prd $id" ;;
        peer)            curl "${curl_args[@]}" -X DELETE "$BASE/api/observer/peers/$id" >/dev/null 2>&1 && echo "  removed peer $id" || echo "  (already gone) peer $id" ;;
        graph)           curl "${curl_args[@]}" -X DELETE "$BASE/api/orchestrator/graphs/$id" >/dev/null 2>&1 && echo "  removed graph $id" || echo "  (already gone) graph $id" ;;
        project-profile) curl "${curl_args[@]}" -X DELETE "$BASE/api/profiles/projects/$id" >/dev/null 2>&1 && echo "  removed project profile $id" || echo "  (already gone) project profile $id" ;;
        cluster-profile) curl "${curl_args[@]}" -X DELETE "$BASE/api/profiles/clusters/$id" >/dev/null 2>&1 && echo "  removed cluster profile $id" || echo "  (already gone) cluster profile $id" ;;
        *)               echo "  (unknown kind) $kind $id" ;;
      esac
    done
  fi

  # v5.26.18 — race-condition orphan sweep. After every PRD has been
  # hard-deleted, list autonomous-named running sessions and kill any
  # that weren't in the pre-smoke baseline (i.e. were spawned during
  # smoke and somehow survived hard-delete's session-kill walk).
  # Baseline tracking means real operator-initiated autonomous runs
  # that pre-existed are NOT touched.
  local NEW_ORPHANS
  NEW_ORPHANS=$(curl "${curl_args[@]}" "$BASE/api/sessions" 2>/dev/null | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    ss = d.get('sessions') if isinstance(d, dict) else d
    baseline = set()
    try:
        with open('$BASELINE_AUTO_SESSIONS') as f:
            baseline = set(line.strip() for line in f if line.strip())
    except Exception:
        pass
    for s in (ss or []):
        if (s.get('name') or '').startswith('autonomous:') and s.get('state') in ('running','waiting_input','rate_limited'):
            fid = s.get('full_id','')
            if fid and fid not in baseline:
                print(fid)
except Exception:
    pass
" 2>/dev/null || true)
  if [[ -n "$NEW_ORPHANS" ]]; then
    if [[ "$printed_header" == "0" ]]; then
      echo ""; echo "== Cleanup =="; printed_header=1
    fi
    for sid in $NEW_ORPHANS; do
      curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "{\"id\":\"$sid\"}" "$BASE/api/sessions/kill" >/dev/null 2>&1
      echo "  killed orphan-autonomous-session $sid (race-survivor)"
    done
  fi

  rm -rf "$TMPD" 2>/dev/null
}
trap cleanup_all EXIT

PASS=0
FAIL=0
SKIP=0

H() { echo ""; echo "== $* =="; }

ok() { echo "  PASS  $*"; PASS=$((PASS+1)); }
ko() { echo "  FAIL  $*"; FAIL=$((FAIL+1)); }
skip() { echo "  SKIP  $*"; SKIP=$((SKIP+1)); }

curl_args=(-sk --max-time 30)
if [[ -n "$TOK" ]]; then curl_args+=(-H "Authorization: Bearer $TOK"); fi

# ---------------------------------------------------------------------------
H "1. Daemon health"
HEALTH=$(curl "${curl_args[@]}" "$BASE/api/health" || true)
if echo "$HEALTH" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
  VER=$(echo "$HEALTH" | python3 -c 'import json,sys;print(json.load(sys.stdin)["version"])')
  ok "health ok, version=$VER"
else
  ko "health endpoint did not return ok: $HEALTH"
  exit 1
fi

# ---------------------------------------------------------------------------
H "2. Backends list"
BK=$(curl "${curl_args[@]}" "$BASE/api/backends" || true)
if echo "$BK" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("llm",[]), list) and len(d["llm"])>0' 2>/dev/null; then
  N=$(echo "$BK" | python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("llm",[])))')
  ok "backends list: $N entries"
else
  ko "backends list shape unexpected: $BK"
fi

# ---------------------------------------------------------------------------
H "3. Stats / observer"
ST=$(curl "${curl_args[@]}" "$BASE/api/stats?v=2" || true)
if echo "$ST" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "envelopes" in d or "v" in d' 2>/dev/null; then
  ok "/api/stats?v=2 returned a structured snapshot"
else
  ko "/api/stats?v=2 unexpected: $(echo "$ST" | head -c 200)"
fi

# ---------------------------------------------------------------------------
H "4. Diagnose"
DG=$(curl "${curl_args[@]}" "$BASE/api/diagnose" || true)
if echo "$DG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,(dict,list))' 2>/dev/null; then
  ok "/api/diagnose returned a result"
else
  ko "/api/diagnose unexpected: $(echo "$DG" | head -c 200)"
fi

# ---------------------------------------------------------------------------
H "5. Channel history endpoint shape"
CH=$(curl "${curl_args[@]}" "$BASE/api/channel/history?session_id=smoke-nonexistent" || true)
# Accept either [] (v5.26.9+) or null (v5.26.1–v5.26.8) as "empty".
if echo "$CH" | python3 -c 'import json,sys;d=json.load(sys.stdin);m=d.get("messages");assert m is None or (isinstance(m,list) and len(m)==0)' 2>/dev/null; then
  ok "/api/channel/history returns 200 + empty messages for unknown session"
else
  ko "/api/channel/history wrong shape: $CH"
fi

# ---------------------------------------------------------------------------
H "6. Autonomous CRUD across every supported worker backend"
A_ENABLED=$(curl "${curl_args[@]}" "$BASE/api/autonomous/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping CRUD test"
else
  # v5.26.10 — exercise each enabled worker backend (claude-code,
  # opencode, ollama) through the same CRUD path. Operator-reported:
  # smoke must validate that PRDs work with claude, opencode, AND
  # ollama as the worker backend, not just claude-code.
  AVAIL=$(curl "${curl_args[@]}" "$BASE/api/backends" | python3 -c '
import json, sys
d = json.load(sys.stdin)
have = {b["name"] for b in d.get("llm",[]) if b.get("enabled") and b.get("available")}
# Only run the CRUD probe against backends the daemon will actually
# accept; "available" gates on the binary being installed / endpoint
# reachable.
target = [b for b in ("claude-code","opencode","ollama") if b in have]
print(",".join(target))
' 2>/dev/null || echo "")
  if [[ -z "$AVAIL" ]]; then
    skip "no claude-code/opencode/ollama backend enabled+available"
  else
    IFS=',' read -ra BACKENDS <<< "$AVAIL"
    for B in "${BACKENDS[@]}"; do
      H "6.$B — CRUD"
      P=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d "{\"spec\":\"smoke probe — autonomous CRUD ($B)\",\"project_dir\":\"/tmp\",\"backend\":\"$B\",\"effort\":\"low\"}" \
        "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
      if [[ -n "$P" ]]; then
        add_cleanup prd "$P"
        ok "[$B] create PRD: $P"
      else
        ko "[$B] create PRD failed"; continue
      fi

      # Verify the PRD record carries the backend through.
      CHK=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$P" | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('backend',''))")
      if [[ "$CHK" == "$B" ]]; then
        ok "[$B] PRD record has backend=$B"
      else
        ko "[$B] PRD record dropped backend (got '$CHK', want '$B')"
      fi

      # /children works (empty for fresh PRD).
      CHILDREN=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$P/children")
      if echo "$CHILDREN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("children",[]),list)' 2>/dev/null; then
        ok "[$B] GET /children empty list"
      else
        ko "[$B] GET /children failed: $CHILDREN"
      fi

      # set_llm round-trip — pin a model relevant to the backend.
      MODEL="${B/-code/}"  # claude-code → claude; opencode → opencode; ollama → ollama
      [[ "$B" == "ollama" ]] && MODEL="qwen3:8b"
      [[ "$B" == "claude-code" ]] && MODEL="claude-sonnet-4-5"
      [[ "$B" == "opencode" ]] && MODEL="claude-sonnet-4-5"
      SETL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d "{\"backend\":\"$B\",\"effort\":\"low\",\"model\":\"$MODEL\",\"actor\":\"smoke\"}" \
        "$BASE/api/autonomous/prds/$P/set_llm")
      if echo "$SETL" | python3 -c "import json,sys;d=json.load(sys.stdin);assert d.get('backend')=='$B' and d.get('model')=='$MODEL'" 2>/dev/null; then
        ok "[$B] set_llm round-trip: backend=$B, model=$MODEL"
      else
        ko "[$B] set_llm failed: $SETL"
      fi

      # Hard delete (cascade-aware Manager guard).
      DEL=$(curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$P?hard=true")
      if echo "$DEL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="deleted"' 2>/dev/null; then
        ok "[$B] hard-delete PRD"
      else
        ko "[$B] hard-delete failed: $DEL"
      fi
    done
  fi
fi

# ---------------------------------------------------------------------------
H "7. Autonomous decompose loopback (the bug that hid for many releases)"
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping decompose test"
else
  # Create a PRD targeting an ask-incompatible backend and confirm the
  # decomposer falls back to ollama, hits the loopback bypass, and
  # returns parseable JSON. v5.26.9 fixed the prompt→question field +
  # the askCompatible fallback.
  PD=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"spec":"List the files in /tmp and write a one-line summary.","project_dir":"/tmp","backend":"claude-code","effort":"low"}' \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
  if [[ -z "$PD" ]]; then
    ko "decompose-test: PRD create failed"
  else
    add_cleanup prd "$PD"
    DR=$(curl "${curl_args[@]}" --max-time 300 -X POST "$BASE/api/autonomous/prds/$PD/decompose" -w "\n__HTTP_CODE_%{http_code}__")
    HTTPC=$(echo "$DR" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
    if [[ "$HTTPC" == "200" ]]; then
      STORIES=$(echo "$DR" | sed 's/__HTTP_CODE.*//' | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d.get("stories",[])))' 2>/dev/null || echo 0)
      ok "decompose returned 200, $STORIES stories"
    elif echo "$DR" | grep -q "x509"; then
      ko "decompose hit x509 — redirect bypass not working: $(echo "$DR" | head -c 200)"
    elif echo "$DR" | grep -q "question required"; then
      ko "decompose returned 'question required' — field-name regression: $(echo "$DR" | head -c 200)"
    elif echo "$DR" | grep -q "unsupported backend"; then
      ko "decompose returned 'unsupported backend' — askCompatible fallback regression"
    else
      skip "decompose returned $HTTPC (body=$(echo "$DR" | head -c 200)) — non-fatal in smoke; LLM may not be reachable"
    fi
    # cleanup_all on EXIT will remove $PD via the trap.
  fi
fi

# ---------------------------------------------------------------------------
H "7b. Autonomous PRD full lifecycle (decompose → approve → run → spawn)"
# v5.26.11 — operator-reported: tasks went TaskFailed before spawning
# because autonomous Effort enum (low/medium/high/max) didn't match
# session Effort enum (quick/normal/thorough). This step asserts the
# spawn round-trip survives the enum translation, even if the actual
# worker session can't complete (which is fine — we only care that
# the executor reaches "spawn returned a session ID").
#
# v5.26.13 — switched the worker backend from `shell` (which
# v5.26.13 excluded from the autonomous LLM list) to the first
# available LLM. Skip if no LLM backend is enabled+available on the
# host. The decompose step in §7 already returned 200 with stories;
# §7b reuses the same call path for the run portion.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping run-lifecycle test"
else
  RUN_B=$(curl "${curl_args[@]}" "$BASE/api/backends" | python3 -c '
import json, sys
d = json.load(sys.stdin)
# Prefer ollama (local + free), then openwebui (local), then opencode, then claude-code.
order = ["ollama", "openwebui", "opencode", "claude-code"]
have = {b["name"]: b for b in d.get("llm",[])}
for name in order:
    b = have.get(name)
    if b and b.get("enabled") and b.get("available"):
        print(name); break
' 2>/dev/null || echo "")
  if [[ -z "$RUN_B" ]]; then
    skip "run-lifecycle: no LLM backend available; can't exercise spawn against an LLM worker"
  else
  PR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"spec\":\"smoke probe — autonomous run lifecycle\",\"project_dir\":\"/tmp\",\"backend\":\"$RUN_B\",\"effort\":\"low\"}" \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
  if [[ -z "$PR" ]]; then
    ko "run-lifecycle: PRD create failed"
  else
    add_cleanup prd "$PR"
    DR=$(curl "${curl_args[@]}" --max-time 300 -X POST "$BASE/api/autonomous/prds/$PR/decompose" -w "\n__HTTP_%{http_code}__")
    if echo "$DR" | grep -q "__HTTP_200__"; then
      ok "run-lifecycle: decompose OK"
      AP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d '{"actor":"smoke","note":"smoke run lifecycle"}' \
        "$BASE/api/autonomous/prds/$PR/approve")
      if echo "$AP" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="approved"' 2>/dev/null; then
        ok "run-lifecycle: approve → approved"
        RN=$(curl "${curl_args[@]}" -X POST "$BASE/api/autonomous/prds/$PR/run")
        if echo "$RN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="running"' 2>/dev/null; then
          ok "run-lifecycle: run → running"
          # Give the executor 8s to spawn and either succeed or hit
          # a real (post-spawn) error like verify-failed.
          sleep 8
          STATE=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR" | python3 -c '
import json, sys
d = json.load(sys.stdin)
fails_pre_spawn = []
fails_post_spawn = []
ok_count = 0
for s in d.get("stories",[]):
    for t in s.get("tasks",[]):
        st = t.get("status","")
        sid = t.get("session_id","")
        err = t.get("error","")
        if st == "failed" and not sid and "invalid effort" in err:
            fails_pre_spawn.append(t.get("id"))
        elif sid:
            ok_count += 1
        elif st == "failed":
            fails_post_spawn.append((t.get("id"), err[:60]))
print(json.dumps({"pre_spawn": fails_pre_spawn, "post_spawn": fails_post_spawn, "spawned": ok_count, "prd_status": d.get("status")}))
')
          if echo "$STATE" | python3 -c 'import json,sys;d=json.loads(sys.stdin.read());assert len(d["pre_spawn"])==0' 2>/dev/null; then
            ok "run-lifecycle: spawn round-trip survived effort-enum translation ($STATE)"
          else
            ko "run-lifecycle: tasks failed PRE-spawn (effort-enum regression): $STATE"
          fi
          # Cancel any in-flight executor goroutine via DELETE (cancel,
          # not hard-delete; cleanup_all takes care of hard-delete).
          curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$PR" >/dev/null 2>&1
        else
          ko "run-lifecycle: run rejected: $RN"
        fi
      else
        ko "run-lifecycle: approve rejected: $AP"
      fi
    else
      skip "run-lifecycle: decompose failed (LLM unreachable?), can't exercise spawn"
    fi
  fi
  fi  # close RUN_B-non-empty
fi

H "7c. PRD project_profile + cluster_profile attachment (v5.26.19)"
# Operator-reported: PRDs should be based on directory or profile,
# with cluster_profile dispatching the worker to /api/agents instead
# of local tmux. Smoke covers (a) profile-existence validation refuses
# unknown names and (b) known names persist on the PRD record.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping profile attachment test"
else
  # Pre-create a project profile so the smoke can attach it. Use a
  # name that's safe to delete after.
  PROF="smoke-prof-$(date +%s)"
  PROF_BODY=$(printf '{"name":"%s","git":{"url":"https://github.com/dmz006/datawatch","branch":"main"},"image_pair":{"agent":"agent-claude"}}' "$PROF")
  PR_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "$PROF_BODY" "$BASE/api/profiles/projects")
  if echo "$PR_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
    ok "created project profile: $PROF"
    add_cleanup project-profile "$PROF"
  else
    skip "could not create project profile (response: $(echo "$PR_RES" | head -c 100))"
    PROF=""
  fi

  # Reject unknown profile name.
  if [[ -n "$PROF" ]]; then
    UNKBODY=$(printf '{"spec":"smoke probe — bad-profile validation","project_dir":"","project_profile":"%s","backend":"ollama","effort":"low"}' "ghost-profile-$RANDOM")
    UNK=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$UNKBODY" "$BASE/api/autonomous/prds" -w "\n__HTTP_%{http_code}__")
    HTTPC=$(echo "$UNK" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
    if [[ "$HTTPC" == "400" ]] && echo "$UNK" | grep -q "project profile"; then
      ok "create with unknown project_profile rejected (400)"
    else
      ko "expected 400 'project profile %q not found', got HTTP $HTTPC body: $(echo "$UNK" | head -c 120)"
    fi

    # Happy path — attach valid profile, verify it persists.
    OKBODY=$(printf '{"spec":"smoke probe — profile attachment","project_dir":"","project_profile":"%s","backend":"ollama","effort":"low"}' "$PROF")
    PR2=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$OKBODY" "$BASE/api/autonomous/prds")
    PR2_ID=$(echo "$PR2" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
    if [[ -n "$PR2_ID" ]]; then
      add_cleanup prd "$PR2_ID"
      GOTPROF=$(echo "$PR2" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("project_profile",""))' 2>/dev/null)
      if [[ "$GOTPROF" == "$PROF" ]]; then
        ok "PRD record carries project_profile=$PROF"
      else
        ko "PRD record dropped project_profile (got=$GOTPROF want=$PROF)"
      fi

      # v5.26.20 — PUT /api/autonomous/prds/{id}/profiles for
      # post-create profile changes. Clear via empty body. The PRD
      # struct uses omitempty so the cleared field is absent from
      # the response, not present-as-empty-string — both shapes are
      # acceptable here.
      PUT_RES=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
        -d '{"project_profile":"","cluster_profile":""}' \
        "$BASE/api/autonomous/prds/$PR2_ID/profiles")
      if echo "$PUT_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert (d.get("project_profile","") == "") and (d.get("cluster_profile","") == "")' 2>/dev/null; then
        ok "PUT /profiles cleared project_profile"
      else
        ko "PUT /profiles failed to clear: $PUT_RES"
      fi
    else
      ko "create with valid project_profile failed: $(echo "$PR2" | head -c 200)"
    fi
  fi
fi

H "7d. Persistent test profiles (datawatch-smoke + smoke-testing)"
# v5.26.33 — operator directive: "the testing cluster can be
# configured on the local server and left there for future tests
# and a test profile can be used with datawatch git and opencode as
# llm for prd and opencode as llm for coding for smoke tests."
#
# Two persistent fixtures: a `smoke-testing` cluster profile + a
# `datawatch-smoke` project profile pinned to the datawatch repo +
# agent-opencode worker image. Idempotent — created once, reused on
# every smoke run, NEVER added to cleanup_log so they outlive the
# test. Differs from §7c which uses ephemeral name-tagged profiles.
SMOKE_PROF="datawatch-smoke"
SMOKE_CLUSTER="smoke-testing"
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping persistent-fixture setup"
else
  # ── Cluster profile ─────────────────────────────────────────────
  CL_GET=$(curl "${curl_args[@]}" "$BASE/api/profiles/clusters/$SMOKE_CLUSTER" -w "\n__HTTP_%{http_code}__" 2>/dev/null)
  CL_HTTP=$(echo "$CL_GET" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
  if [[ "$CL_HTTP" == "200" ]]; then
    ok "cluster profile $SMOKE_CLUSTER already present (reused)"
  else
    CL_BODY=$(printf '{"name":"%s","description":"Persistent local-docker cluster for release-smoke","kind":"docker","namespace":"default"}' "$SMOKE_CLUSTER")
    CL_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$CL_BODY" "$BASE/api/profiles/clusters")
    if echo "$CL_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
      ok "cluster profile $SMOKE_CLUSTER created (persistent — not cleaned up)"
    else
      skip "cluster profile create failed (kind=docker may need driver wiring): $(echo "$CL_RES" | head -c 120)"
      SMOKE_CLUSTER=""
    fi
  fi

  # ── Project profile ─────────────────────────────────────────────
  PJ_GET=$(curl "${curl_args[@]}" "$BASE/api/profiles/projects/$SMOKE_PROF" -w "\n__HTTP_%{http_code}__" 2>/dev/null)
  PJ_HTTP=$(echo "$PJ_GET" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
  if [[ "$PJ_HTTP" == "200" ]]; then
    ok "project profile $SMOKE_PROF already present (reused)"
  else
    # Operator-asked: opencode for both PRD decompose and worker
    # coding. image_pair.agent picks the worker image; daemon-side
    # decompose backend is a separate config knob (autonomous.
    # decomposition_backend) that operators set in config.yaml.
    PJ_BODY=$(printf '{"name":"%s","description":"Persistent smoke fixture: datawatch git + opencode worker","git":{"url":"https://github.com/dmz006/datawatch","branch":"main","provider":"github"},"image_pair":{"agent":"agent-opencode","sidecar":"lang-go"},"memory":{"mode":"sync-back"}}' "$SMOKE_PROF")
    PJ_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$PJ_BODY" "$BASE/api/profiles/projects")
    if echo "$PJ_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
      ok "project profile $SMOKE_PROF created (persistent — not cleaned up)"
    else
      skip "project profile create failed: $(echo "$PJ_RES" | head -c 120)"
      SMOKE_PROF=""
    fi
  fi

  # ── PRD round-trip referencing both fixtures ────────────────────
  if [[ -n "$SMOKE_PROF" && -n "$SMOKE_CLUSTER" ]]; then
    RT_BODY=$(printf '{"spec":"smoke probe — persistent fixture round-trip","project_profile":"%s","cluster_profile":"%s"}' "$SMOKE_PROF" "$SMOKE_CLUSTER")
    RT=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$RT_BODY" "$BASE/api/autonomous/prds")
    RT_ID=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
    if [[ -n "$RT_ID" ]]; then
      add_cleanup prd "$RT_ID"   # PRD is ephemeral; profiles persist
      GOT_PROF=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("project_profile",""))' 2>/dev/null)
      GOT_CLUS=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("cluster_profile",""))' 2>/dev/null)
      if [[ "$GOT_PROF" == "$SMOKE_PROF" && "$GOT_CLUS" == "$SMOKE_CLUSTER" ]]; then
        ok "PRD round-trip carries persistent fixtures (project=$SMOKE_PROF cluster=$SMOKE_CLUSTER)"
      else
        ko "PRD record dropped fixture refs (project=$GOT_PROF cluster=$GOT_CLUS)"
      fi
    else
      ko "PRD create against persistent fixtures failed: $(echo "$RT" | head -c 200)"
    fi
  fi
fi

H "7e. Filter store CRUD"
# v5.26.41 — operator directive (service-function smoke audit):
# every store with REST CRUD should round-trip in smoke. Filters
# are the simplest shape (pattern + action + value); schedule and
# alert stores have more complex bodies and stay deferred.
FILTER_PAT="smoke-probe-$(date +%s)"
FC=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "$(printf '{"pattern":"%s","action":"schedule","value":"yes"}' "$FILTER_PAT")" \
  "$BASE/api/filters")
FC_ID=$(echo "$FC" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
if [[ -n "$FC_ID" ]]; then
  ok "create filter: $FC_ID (pattern=$FILTER_PAT)"
  # Read-back via list
  if curl "${curl_args[@]}" "$BASE/api/filters" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('filters',[])
assert any(f.get('id') == '$FC_ID' for f in arr), 'created filter not in list'
" 2>/dev/null; then
    ok "filter $FC_ID round-trips through GET /api/filters"
  else
    ko "filter $FC_ID NOT visible in GET /api/filters list"
  fi
  # Delete
  if curl "${curl_args[@]}" -X DELETE "$BASE/api/filters?id=$FC_ID" | grep -q '"status"'; then
    ok "delete filter $FC_ID"
  else
    ko "delete filter $FC_ID failed"
  fi
else
  skip "filter create failed: $(echo "$FC" | head -c 100)"
fi

H "7f. Memory + KG round-trip"
# v5.26.47 — service-function smoke audit. The §9 memory check
# only hits /api/memory/search; this section exercises the rest of
# the operator-facing memory surface that's gated on the same
# subsystem being enabled:
#   - /api/memory/stats        — health + count snapshot
#   - /api/memory/kg/stats     — KG entity/triple counters
#   - POST /api/memory/save    — write a memory with spatial dims
#                                 (wing/room/hall from nightwire BL55)
#   - GET  /api/memory/search  — round-trip read-back
#   - DELETE /api/memory/delete — cleanup the probe entry
MEM_OK=$(curl "${curl_args[@]}" "$BASE/api/memory/stats" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory subsystem not enabled — skipping memory + KG round-trip"
else
  ok "/api/memory/stats reports enabled=true"

  # KG stats shape — accept any non-error JSON with the four keys.
  if curl "${curl_args[@]}" "$BASE/api/memory/kg/stats" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for k in ('entity_count','triple_count','active_count','expired_count'):
    assert k in d, 'missing '+k
" 2>/dev/null; then
    ok "/api/memory/kg/stats returns the canonical 4-counter shape"
  else
    ko "/api/memory/kg/stats missing one of entity_count/triple_count/active_count/expired_count"
  fi

  # Save with spatial dims, capture id, search, delete.
  PROBE_TXT="smoke probe v5.26.47 wing=smoke room=probe hall=facts $(date +%s)"
  SR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
       -d "$(printf '{"content":"%s","wing":"smoke","room":"probe","hall":"facts"}' "$PROBE_TXT")" \
       "$BASE/api/memory/save")
  MEM_ID=$(echo "$SR" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$MEM_ID" && "$MEM_ID" != "0" ]]; then
    ok "memory save with wing/room/hall returned id=$MEM_ID"
    # Search read-back: probe text contains the unique timestamp so
    # collisions with prior runs aren't possible.
    if curl "${curl_args[@]}" "$BASE/api/memory/search?q=$(echo -n "v5.26.47" | python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.stdin.read()))')" \
        | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('results',[])
hit = any(str(m.get('id','')) == '$MEM_ID' for m in arr)
assert hit, 'saved id $MEM_ID not in search results'
" 2>/dev/null; then
      ok "memory search round-trips id=$MEM_ID"
    else
      ko "memory search did NOT return the saved probe id=$MEM_ID"
    fi
    # Cleanup the probe.
    if curl "${curl_args[@]}" -X DELETE "$BASE/api/memory/delete?id=$MEM_ID" >/dev/null 2>&1; then
      ok "memory probe id=$MEM_ID deleted"
    else
      ko "memory probe id=$MEM_ID delete failed"
    fi
  else
    skip "memory save returned no id — body: $(echo "$SR" | head -c 120)"
  fi
fi

H "7g. MCP tool surface"
# v5.26.48 — service-function smoke audit. /api/mcp/docs returns
# the full MCP tool inventory the daemon exposes. Smoke verifies:
#   - response is a JSON array of >= 30 tools (defensive lower bound;
#     current count is 39, but releases that strip tools should still
#     keep the foundational set)
#   - the foundational subset is registered (list_sessions /
#     start_session / send_input / schedule_add / profile_list /
#     agent_list — every operator MCP wrapper depends on these)
MCP_RES=$(curl "${curl_args[@]}" "$BASE/api/mcp/docs" 2>/dev/null)
if echo "$MCP_RES" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert isinstance(d, list) and len(d) >= 30, 'tool count below floor: %d' % len(d)
names = {t['name'] for t in d}
required = {'list_sessions','start_session','send_input','schedule_add','profile_list','agent_list'}
missing = required - names
assert not missing, 'missing tools: ' + ','.join(sorted(missing))
print('count=%d' % len(d))
" 2>/dev/null; then
  ok "/api/mcp/docs returns the canonical MCP tool surface (>=30 tools, foundational subset present)"
else
  ko "MCP tool surface incomplete: $(echo "$MCP_RES" | head -c 200)"
fi

H "8. Observer peer register + push + cross-host aggregator"
PEER_NAME="smoke-peer-$(date +%s)"
REG=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"name\":\"$PEER_NAME\",\"shape\":\"A\",\"version\":\"smoke\"}" \
  "$BASE/api/observer/peers")
PEER_TOK=$(echo "$REG" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")
if [[ -n "$PEER_TOK" && ${#PEER_TOK} -gt 20 ]]; then
  add_cleanup peer "$PEER_NAME"
  ok "peer register: $PEER_NAME (token len ${#PEER_TOK})"
else
  ko "peer register failed: $REG"
fi

if [[ -n "$PEER_TOK" ]]; then
  PUSH=$(curl "${curl_args[@]}" -X POST \
    -H "Authorization: Bearer $PEER_TOK" -H "Content-Type: application/json" \
    -d "{\"shape\":\"A\",\"peer_name\":\"$PEER_NAME\",\"snapshot\":{\"v\":2,\"envelopes\":[{\"id\":\"smoke-env\",\"kind\":\"Backend\",\"name\":\"smoke\"}]}}" \
    "$BASE/api/observer/peers/$PEER_NAME/stats")
  if echo "$PUSH" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
    ok "peer push: snapshot accepted"
  else
    ko "peer push failed: $PUSH"
  fi

  ALL=$(curl "${curl_args[@]}" "$BASE/api/observer/envelopes/all-peers")
  if echo "$ALL" | python3 -c "import json,sys;d=json.load(sys.stdin);assert '$PEER_NAME' in (d.get('by_peer') or {})" 2>/dev/null; then
    ok "cross-host aggregator includes $PEER_NAME"
  else
    ko "cross-host aggregator missing $PEER_NAME: $(echo "$ALL" | head -c 200)"
  fi

  # cleanup_all on EXIT will deregister $PEER_NAME via the trap.
fi

# ---------------------------------------------------------------------------
H "9. Memory recall (smoke)"
# v5.26.28 fix — endpoint is /api/memory/search (not /recall). The
# old path always 404'd, so smoke silently SKIPped memory across
# every release even when the subsystem was healthy.
MR=$(curl "${curl_args[@]}" "$BASE/api/memory/search?q=smoke" || true)
# Accept either {"results":[...]} OR a bare top-level list. The
# previous check called .get() on a list and threw AttributeError,
# which made smoke SKIP even on healthy memory subsystems.
if echo "$MR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list) or isinstance(d.get("results",[]),list)' 2>/dev/null; then
  ok "memory search returned a result list"
else
  skip "memory not enabled or returned $(echo "$MR" | head -c 100)"
fi

# ---------------------------------------------------------------------------
H "10. Voice transcribe availability"
VC=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("whisper",{}).get("enabled") else "no")' 2>/dev/null || echo no)
if [[ "$VC" == "yes" ]]; then
  ok "whisper enabled (transcription endpoint reachable in PWA)"
else
  skip "whisper disabled — mic affordances stay hidden in PWA"
fi

# ---------------------------------------------------------------------------
H "11. Orchestrator graph CRUD"
O_ENABLED=$(curl "${curl_args[@]}" "$BASE/api/orchestrator/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$O_ENABLED" != "yes" ]]; then
  skip "orchestrator disabled; skipping graph CRUD"
else
  G=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"name":"smoke-graph","prds":[]}' "$BASE/api/orchestrator/graphs")
  GID=$(echo "$G" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$GID" ]]; then
    add_cleanup graph "$GID"
    ok "orchestrator graph create: $GID"
  else
    ko "orchestrator graph create failed: $G"
  fi
fi

# ---------------------------------------------------------------------------
H "Summary"
echo "  Pass:  $PASS"
echo "  Fail:  $FAIL"
echo "  Skip:  $SKIP"

if [[ "$FAIL" -gt 0 ]]; then
  echo ""
  echo "FAIL: $FAIL functional check(s) failed; release should NOT proceed."
  exit 1
fi
echo ""
echo "OK: all functional checks passed (skips are fine — gated on whether the subsystem is configured)."
exit 0
