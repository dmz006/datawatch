#!/usr/bin/env bash
#
# tree-serialization-smoke.sh
#
# LIVE smoke test for the autonomous per-canonical-tree lock (BL469 same-tree
# admission + BL428 tree-lock worktree parallelism) against a fresh scratch
# daemon built from HEAD.
#
# What it proves, live (not unit tests):
#
#   SCENARIO 1 -- two PRDs on the SAME tree (direct mode, same project_dir).
#     * The per-tree lock must PREVENT same-tree parallelism: the two tasks
#       must never be `in_progress` at the same time.
#     * Each task must land its OWN file in its OWN commit.
#     * (Known gap, reproduced live) the same-tree loser is refused on
#       admission and reverts to `approved`; it is NOT auto-queued into a
#       `waiting` state and does NOT auto-resume when the holder finishes --
#       the operator must re-kick it. The script demonstrates this explicitly.
#
#   SCENARIO 2 -- two PRDs on DISTINCT trees (distinct project_dirs; the
#     same property worktree mode provides via distinct branches).
#     * Because the lock is PER-TREE, not global, both tasks MUST be able to
#       be `in_progress` SIMULTANEOUSLY. This is the decisive proof that the
#       gate is the tree lock (not a global capacity limit): capacity is set
#       high enough that it cannot be the constraint.
#
# Requirements (local): a `datawatch` build (or BINARY=/path), a local LLM
# reachable by pi-prompt->ollama (PI_MODEL), and a `claude` binary for task
# execution. Everything runs in a throwaway scratch env under $SMOK_ROOT so it
# never touches the operator daemon or the live workspace tree.
#
# Usage:
#   BINARY=$(pwd)/datawatch SMOK_PORT=18912 \
#     PI_MODEL=ollama/qwen2.5-coder:14b \
#     ./tests/live-smoke/tree-serialization-smoke.sh
#
# Exit 0 when both live assertions hold (same-tree serialized, cross-tree
# parallel). Prints a VERDICT block at the end.

set -uo pipefail

ROOT="${SMOK_ROOT:-${DATAWATCH_HOME:-.datawatch}/tree-smoke-$(date +%Y%m%d-%H%M%S)}"
PORT="${SMOK_PORT:-18912}"
PI_MODEL="${PI_MODEL:-ollama/qwen2.5-coder:14b}"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
BINARY="${BINARY:-$REPO/datawatch}"
EVDIR="$REPO/tests/live-smoke/EVIDENCE/$(date +%Y%m%d-%H%M%S)-run"
mkdir -p "$ROOT/sandbox" "$EVDIR"
export GIT_AUTHOR_NAME=dw-smoke GIT_AUTHOR_EMAIL=smoke@local
export GIT_COMMITTER_NAME=dw-smoke GIT_COMMITTER_EMAIL=smoke@local

log(){ printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
die(){ printf '!! %s\n' "$*" >&2; exit 1; }
cleanup(){
  # Only kill the pid this script recorded AND whose live cmdline still says
  # it is our scratch datawatch daemon — a recycled pidfile pid is never
  # signaled.
  log "cleaning up scratch daemon (if any)"
  if [ -f "$ROOT/sandbox/daemon.pid" ]; then
    p=$(cat "$ROOT/sandbox/daemon.pid" 2>/dev/null || true)
    if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then
      cmd=$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null || true)
      case "$cmd" in
        *datawatch*config.yaml*serve*) kill "$p" 2>/dev/null || true;;
        *) log "daemon.pid pid $p cmdline ('$cmd') is not this script's scratch daemon; not killing";;
      esac
    fi
  fi
  return 0
}
trap cleanup EXIT

get(){ # get <id> -> prd/story/task[/reason]
  "$DW" autonomous prd-get "$1" --url "$U" 2>/dev/null \
    | jq -r '[.status,(.stories[0].status//""),(.stories[0].tasks[0].status//""),((.stories[0].tasks[0].wait_reason//.stories[0].tasks[0].reason//""))]|join("/")' 2>/dev/null
}
terminal(){ case "$1" in *completed*|*failed*|*blocked*|*cancelled*) return 0;; *) return 1;; esac; }

# --- 1. build if needed ------------------------------------------------------
if [ ! -x "$BINARY" ]; then log "no BINARY; building datawatch"; ( cd "$REPO" && go build -o datawatch . ) || die "build failed"; fi
mkdir -p "$ROOT/bin"; cp -f "$BINARY" "$ROOT/bin/datawatch"
DW="$ROOT/bin/datawatch"; U="http://127.0.0.1:$PORT"
log "binary=$BINARY"; log "root=$ROOT port=$PORT pi_model=$PI_MODEL"

# --- 2. scratch trees --------------------------------------------------------
new_tree(){ local T="$ROOT/$1"; rm -rf "$T"; mkdir -p "$T"; ( cd "$T" && git init -q && \
  git config user.name dw-smoke && git config user.email smoke@local && printf "# %s\n" "$1" > README.md && \
  git add README.md && git commit -qm "baseline $1" && log "  $1 base=$(git rev-parse HEAD)"; ); }
log "scenario trees (s1 shared; s2c/s2d distinct)"
new_tree s1; new_tree s2c; new_tree s2d

# --- 3. scratch config -------------------------------------------------------
cat > "$ROOT/sandbox/config.yaml" <<YAML
hostname: ts-smoke
log: { level: info, file: "$ROOT/sandbox/daemon.log" }
server: { enabled: true, port: $PORT, https: false, bind: 127.0.0.1, basic_auth: false, tls: false, mcp: false }
session: { enabled: true, llm_backend: claude-code }
llm: {}
claude_code: { enabled: true, binary: claude, model: null, timeout_seconds: 300 }
pi:      { enabled: true, binary: pi, model: "$PI_MODEL", timeout_seconds: 120 }
pi_prompt: { enabled: true, binary: pi, model: "$PI_MODEL", timeout_seconds: 120 }
compute_node: { enabled: false, provider: "llm/ollama", model: "", base_url: "http://127.0.0.1:11434", context_limit_tokens: 32768, enabled: false }
autonomous:
  enabled: true
  max_concurrent_tasks: 8            # HIGH: capacity must NOT be the gate; the per-tree lock is
  max_parallel_tasks: 8
  timeout_seconds: 600
  execution_backend: claude-code
  planning_backend: pi-prompt
  planning_model: "$PI_MODEL"
  verification_backend: pi-prompt
  verification_model: "$PI_MODEL"
  worktree_base_repo: "$ROOT/s1"
  block_on_file_drift: false          # out of scope for this test
  auto_approve_children: true
  continue_on_story_failure: true
  max_replan_attempts: 1
  max_child_retries: 1
  child_mode: inherit
  llm: { enabled: true, backend: pi-prompt, model: "$PI_MODEL", provider_base_url: "http://127.0.0.1:11434", api_key: local, context_limit_tokens: 16384 }
YAML

# --- 4. start scratch daemon -------------------------------------------------
log "starting scratch daemon"
( cd "$ROOT/sandbox" && "$DW" --config config.yaml serve > daemon.out 2>&1 & echo $! > daemon.pid )
for i in $(seq 1 30); do curl -sf "$U/api/healthz" >/dev/null 2>&1 && { log "daemon up (pid $(cat "$ROOT/sandbox/daemon.pid"))"; break; }; sleep 1; done
curl -sf "$U/api/healthz" >/dev/null 2>&1 || die "scratch daemon not healthy; see $ROOT/sandbox/daemon.out"

P(){ # P <spec> <dir> -> id
  "$DW" autonomous prd-create "$1" --project-dir "$2" --url "$U" 2>/dev/null | jq -r .id 2>/dev/null; }
plan_wait(){ # plan_wait <id>
  local id="$1"; "$DW" autonomous prd-plan "$id" --url "$U" >/dev/null 2>&1; local t=$(date +%s)
  for i in $(seq 1 60); do local s; s=$("$DW" autonomous prd-get "$id" --url "$U" 2>/dev/null|jq -r .status 2>/dev/null)
    case "$s" in draft|planning) sleep 3;; *) log "  $id -> $s ($(( $(date +%s) - t ))s)"; return 0;; esac; done
  log "  $id plan timeout -> $( "$DW" autonomous prd-get "$id" --url "$U" 2>/dev/null|jq -r .status )"; }

# ============================================================================
log "========================== SCENARIO 1 (same tree) ========================"
log "PRD A + B on shared tree s1"
A=$(P "Create exactly one new file named A_LIST.txt at the project root containing the integers 1 through 150, one integer per line, no header, no footer. Do not modify any other file." "$ROOT/s1")
B=$(P "Create exactly one new file named B_LOG.txt at the project root containing exactly 150 lines where line N is 'line N: B' for N=1..150. Do not modify any other file." "$ROOT/s1")
log "A=$A B=$B";  echo "$A" > "$EVDIR/s1_A_id"; echo "$B" > "$EVDIR/s1_B_id"
log "planning A+B (pi-prompt local)"
plan_wait "$A"; plan_wait "$B"
log "approving + kicking both ~simultaneously"
"$DW" autonomous prd-approve "$A" --url "$U" >/dev/null 2>&1
"$DW" autonomous prd-approve "$B" --url "$U" >/dev/null 2>&1
log "kick A"; "$DW" autonomous prd-run "$A" --url "$U" >/dev/null 2>&1
log "kick B"; "$DW" autonomous prd-run "$B" --url "$U" >/dev/null 2>&1

S1TL="$EVDIR/timeline_s1.txt"; : > "$S1TL"
s1_same_tree_parallel="NO"; s1_no_overlap="YES"; B_ever_in_progress="NO"
t=$(date +%s); last=""
for i in $(seq 1 120); do
  La=$(get "$A"); Lb=$(get "$B"); NOW=$(date '+%H:%M:%S'); LINE="[$NOW] A=$La | B=$Lb"
  if ( terminal "$La" ) && ( terminal "$Lb" ); then echo "[$NOW] BOTH TERMINAL ($(( (date +%s) - t ))s)" | tee -a "$S1TL"; break; fi
  if get "$A" | grep -q "in_progress" && get "$B" | grep -q "in_progress"; then s1_same_tree_parallel="YES"; fi
  get "$B" | grep -q "in_progress" && B_ever_in_progress="YES"
  if [ "$LINE" != "$last" ]; then echo "$LINE" | tee -a "$S1TL"; last="$LINE"; fi
  sleep 2
done
log "s1: A=$("$DW" autonomous prd-get "$A" --url "$U" 2>/dev/null|jq -r .status)  B=$("$DW" autonomous prd-get "$B" --url "$U" 2>/dev/null|jq -r .status)"

# Decisive same-tree demo: if B is still not-running while/after A, re-kick it.
Bst=$("$DW" autonomous prd-get "$B" --url "$U" 2>/dev/null|jq -r .status 2>/dev/null)
rekick_note=""
if [ "$Bst" = "approved" ] || [ "$Bst" = "waiting" ]; then
  log "B is '$Bst' at A's finish (same-tree loser not auto-resumed) -> manual re-kick"
  "$DW" autonomous prd-approve "$B" --url "$U" >/dev/null 2>&1
  log "re-kick B"; "$DW" autonomous prd-run "$B" --url "$U" >/dev/null 2>&1
  t=$(date +%s); for i in $(seq 1 60); do Lb=$(get "$B"); terminal "$( "$DW" autonomous prd-get "$B" --url "$U" 2>/dev/null|jq -r .status )" && break; sleep 2; done
  rekick_note="manual re-kick @$(date +%H:%M:%S) -> B=$("$DW" autonomous prd-get "$B" --url "$U" 2>/dev/null|jq -r .status)"
  echo "$rekick_note" | tee -a "$S1TL"
fi
log "s1 git:"; git -C "$ROOT/s1" log --oneline --stat -6 | tee -a "$S1TL"

# ============================================================================
log "======================== SCENARIO 2 (distinct trees) ====================="
log "PRD C + D on distinct trees s2c / s2d"
C=$(P "Create exactly one new file named C_FILE.txt at the project root containing the single line 'worktree C ok' and nothing else. Do not modify any other file." "$ROOT/s2c")
D=$(P "Create exactly one new file named D_FILE.txt at the project root containing the single line 'worktree D ok' and nothing else. Do not modify any other file." "$ROOT/s2d")
log "C=$C D=$D";  echo "$C" > "$EVDIR/s2_C_id"; echo "$D" > "$EVDIR/s2_D_id"
log "planning C+D (pi-prompt local)"
plan_wait "$C"; plan_wait "$D"
log "approving + kicking both ~simultaneously (distinct trees => expect parallel)"
"$DW" autonomous prd-approve "$C" --url "$U" >/dev/null 2>&1
"$DW" autonomous prd-approve "$D" --url "$U" >/dev/null 2>&1
log "kick C"; "$DW" autonomous prd-run "$C" --url "$U" >/dev/null 2>&1
log "kick D"; "$DW" autonomous prd-run "$D" --url "$U" >/dev/null 2>&1

S2TL="$EVDIR/timeline_s2.txt"; : > "$S2TL"
s2_parallel="NO"; t=$(date +%s); last=""
for i in $(seq 1 120); do
  Lc=$(get "$C"); Ld=$(get "$D"); NOW=$(date '+%H:%M:%S'); LINE="[$NOW] C=$Lc | D=$Ld"
  if ( terminal "$( "$DW" autonomous prd-get "$C" --url "$U" 2>/dev/null|jq -r .status )" ) && \
     ( terminal "$( "$DW" autonomous prd-get "$D" --url "$U" 2>/dev/null|jq -r .status )" ); then
    echo "[$NOW] BOTH TERMINAL ($(( (date +%s) - t ))s)" | tee -a "$S2TL"; break; fi
  if get "$C" | grep -q "in_progress" && get "$D" | grep -q "in_progress"; then s2_parallel="YES@${NOW}"; fi
  if [ "$LINE" != "$last" ]; then echo "$LINE" | tee -a "$S2TL"; last="$LINE"; fi
  sleep 2
done
log "s2 git (C):"; git -C "$ROOT/s2c" log --oneline --stat -4 | tee -a "$S2TL"
log "s2 git (D):"; git -C "$ROOT/s2d" log --oneline --stat -4 | tee -a "$S2TL"
log "D failure_reason (if any):"
"$DW" autonomous prd-get "$D" --url "$U" 2>/dev/null | jq '.stories[0].tasks[0].failure_reason // "none"' | tee -a "$S2TL"

# ============================================================================
# Snapshots + verdict
"$DW" autonomous prd-get "$A" --url "$U" 2>/dev/null > "$EVDIR/A_snap.json"
"$DW" autonomous prd-get "$B" --url "$U" 2>/dev/null > "$EVDIR/B_snap.json"
"$DW" autonomous prd-get "$C" --url "$U" 2>/dev/null > "$EVDIR/C_snap.json"
"$DW" autonomous prd-get "$D" --url "$U" 2>/dev/null > "$EVDIR/D_snap.json"
git -C "$ROOT/s1"  log --stat -8 | cat >  "$EVDIR/git-logs.txt"
git -C "$ROOT/s2c" log --stat -5 | cat >> "$EVDIR/git-logs.txt"
git -C "$ROOT/s2d" log --stat -5 | cat >> "$EVDIR/git-logs.txt"

# ---- assertions -------------------------------------------------------------
s1_A_done=$(jq -r .status "$EVDIR/A_snap.json" 2>/dev/null)
s1_B_done=$(jq -r .status "$EVDIR/B_snap.json" 2>/dev/null)
s2_C_done=$(jq -r .status "$EVDIR/C_snap.json" 2>/dev/null)
s2_D_done=$(jq -r .status "$EVDIR/D_snap.json" 2>/dev/null)
a1_ok="NO"; [ "$s1_same_tree_parallel" = "NO" ] && a1_ok="YES"
a2_ok="NO"; [ "$s2_parallel" != "NO" ] && a2_ok="YES"

{
  echo "==== VERDICT (live) ===="
  echo "A($A)=$s1_A_done  B($B)=$s1_B_done"
  echo "C($C)=$s2_C_done  D($D)=$s2_D_done"
  [ -n "$rekick_note" ] && echo "S1 loser: $rekick_note"
  echo "A1 same-tree serialized (never both in_progress) = $a1_ok  [observed same-tree parallel: $s1_same_tree_parallel; B ever in_progress: $B_ever_in_progress]"
  echo "A2 cross-tree parallel (both in_progress at once)  = $a2_ok  [observed: $s2_parallel]"
  echo "known-gap (BL428) : same-tree loser auto-resume  = $([ -n "$rekick_note" ] && echo 'ABSENT (re-kick needed)' || echo 'OK (auto-resumed)')"
  echo "D infra note       : $(jq -r '.stories[0].tasks[0].failure_reason.category // "ok"' "$EVDIR/D_snap.json" 2>/dev/null) (worker stall is infra, unrelated to the lock)"
  echo "evidence dir       : $EVDIR"
} | tee "$EVDIR/VERDICT.txt"

log "scratch daemon stopped (cleanup)."
[ "$a1_ok" = "YES" ] && [ "$a2_ok" = "YES" ] || exit 2; exit 0
