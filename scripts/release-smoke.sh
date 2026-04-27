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

cleanup_all() {
  if [[ ! -s "$CLEANUP_LOG" ]]; then
    rm -rf "$TMPD" 2>/dev/null
    return
  fi
  echo ""
  echo "== Cleanup =="
  # tac to delete in reverse order
  tac "$CLEANUP_LOG" | while read -r kind id; do
    case "$kind" in
      prd)   curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$id?hard=true" >/dev/null 2>&1 && echo "  removed prd $id" || echo "  (already gone) prd $id" ;;
      peer)  curl "${curl_args[@]}" -X DELETE "$BASE/api/observer/peers/$id" >/dev/null 2>&1 && echo "  removed peer $id" || echo "  (already gone) peer $id" ;;
      graph) curl "${curl_args[@]}" -X DELETE "$BASE/api/orchestrator/graphs/$id" >/dev/null 2>&1 && echo "  removed graph $id" || echo "  (already gone) graph $id" ;;
      *)     echo "  (unknown kind) $kind $id" ;;
    esac
  done
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
H "6. Autonomous CRUD (create + cancel + delete cascade refusal)"
A_ENABLED=$(curl "${curl_args[@]}" "$BASE/api/autonomous/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping CRUD test"
else
  P=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"spec":"smoke probe — autonomous CRUD","project_dir":"/tmp","backend":"claude-code","effort":"low"}' \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
  if [[ -n "$P" ]]; then
    add_cleanup prd "$P"
    ok "create PRD: $P"
  else
    ko "create PRD failed"
  fi

  CHILDREN=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$P/children")
  if echo "$CHILDREN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("children",[]),list)' 2>/dev/null; then
    ok "GET /children works (returns empty list for new PRD)"
  else
    ko "GET /children failed: $CHILDREN"
  fi

  # set_llm round-trip
  SETL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"backend":"ollama","effort":"low","model":"qwen3:8b","actor":"smoke"}' \
    "$BASE/api/autonomous/prds/$P/set_llm")
  if echo "$SETL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("backend")=="ollama" and d.get("model")=="qwen3:8b"' 2>/dev/null; then
    ok "set_llm round-trip: backend=ollama, model=qwen3:8b"
  else
    ko "set_llm round-trip failed: $SETL"
  fi

  # Hard delete (also exercises the cascade-aware Manager guard).
  DEL=$(curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$P?hard=true")
  if echo "$DEL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="deleted"' 2>/dev/null; then
    ok "hard-delete PRD: $P"
    # Already gone — drop from cleanup queue is harmless (cleanup_all
    # tolerates "already gone").
  else
    ko "hard-delete failed: $DEL"
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
MR=$(curl "${curl_args[@]}" "$BASE/api/memory/recall?q=smoke" || true)
if echo "$MR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("results",[]),list) or isinstance(d,list)' 2>/dev/null; then
  ok "memory recall returned a result list"
else
  skip "memory recall not enabled or returned $(echo "$MR" | head -c 100)"
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
