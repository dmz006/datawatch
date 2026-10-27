#!/usr/bin/env bash
# release-smoke-automatic-commit.sh — end-to-end smoke for automatic session completion commits
#
# Operator-directed (2026-07-14): after every one-task run we want
# confirmation that the auto-commit path actually produces the commit.
# Unlike release-smoke.sh this drives REAL worker sessions against a
# SANDBOX daemon and asserts on the git repo the daemon left behind.
#
# What it proves:
#   (a) a one-shot session whose worker creates a file finishes with the
#       repo holding exactly one commit whose commit message is the
#       worker's DATAWATCH_COMPLETE: summary, and the worker did not run
#       git commit on its own;
#   (b) re-running the same task against the already-committed repo ends
#       again without adding any second commit (worker made no new
#       change → daemon must not commit anything).
#
# SAFETY: this script NEVER points at a production port. The target
# daemon base URL must resolve to loopback (127.0.0.1 / localhost / ::1).
# Anything else is refused outright.
#
# Usage:
#   scripts/release-smoke-automatic-commit.sh
#
# Env vars:
#   DW_BASE               sandbox daemon base URL (default: http://127.0.0.1:9090)
#   DW_BASE_TOKEN         bearer token for the sandbox daemon (optional)
#   DW_LLM_BACKEND        LLM backend to run the worker sessions with
#                         (default: opencode-acp — free, local; set
#                         DW_LLM_BACKEND=claude-code or DW_MAJOR=1 to
#                         spend API quota instead)
#   DW_MAJOR              set to 1 to allow claude-code when the default
#                         free backend is unavailable (cost gate, matches
#                         release-smoke.sh's convention)
#   SMOKE_TIMEOUT_SECONDS completion wait per session (default: 240)
#   KEEP_TEST_DIR         set to 1 to keep the scratch git repo for
#                         inspection (default: deleted on exit)
#
# Exit codes:
#   0 — all checks passed (or cleanly skipped: no sandbox configured,
#       or no LLM backend available and cost gate closed)
#   1 — one or more checks failed
#   2 — refused to run at all (non-loopback target, missing tooling)
set -euo pipefail

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

ok()   { printf '  [PASS] %s\n' "$1"; PASS_COUNT=$((PASS_COUNT + 1)); }
ko()   { printf '  [FAIL] %s\n' "$1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
skip() { printf '  [SKIP] %s\n' "$1"; SKIP_COUNT=$((SKIP_COUNT + 1)); }
H()    { printf '\n\033[1;36m=== %s ===\033[0m\n' "$1"; }

# ---------------------------------------------------------------------------
# Target resolution + sandbox guard
# ---------------------------------------------------------------------------
BASE="${DW_BASE:-http://127.0.0.1:9090}"
BASE="${BASE%/}"

# Sandbox-only guard: refuse any non-loopback host before touching anything.
HOST=$(printf '%s' "$BASE" | sed -n 's#^https\{0,1\}://\([^:/]*\).*#\1#p' | tr 'A-Z' 'a-z')
case "$HOST" in
  ""|127.0.0.1|localhost|"::1"|\[::1\]) : ;;
  *)
    printf '[REFUSED] target daemon is not loopback: %s\n' "$BASE" >&2
    printf 'This smoke only runs against a SANDBOX daemon (127.0.0.1/localhost).\n' >&2
    exit 2
    ;;
esac

TOKEN="${DW_BASE_TOKEN:-}"
CURL_ARGS=(-sk --max-time 20) # -k: the sandbox daemon serves a self-signed TLS cert
[[ -n "$TOKEN" ]] && CURL_ARGS+=(-H "Authorization: Bearer $TOKEN")

# ---------------------------------------------------------------------------
# Is there a sandbox daemon at all? (clear skip message if not)
# ---------------------------------------------------------------------------
HEALTH=$(curl "${CURL_ARGS[@]}" "$BASE/api/health" 2>/dev/null || echo "")
if ! echo "$HEALTH" | grep -qiE '"status"\s*:\s*"(ok|healthy|up)"'; then
  printf '  [SKIP] no sandbox datawatch daemon reachable at %s — automatic-commit smoke skipped\n' "$BASE"
  printf '         Start a sandbox daemon (or point DW_BASE/DW_BASE_TOKEN at one) and re-run.\n'
  SKIP_COUNT=$((SKIP_COUNT + 1))
  printf '\n=== RESULTS ===\n'
  printf 'PASS: %d\nFAIL: %d\nSKIP: %d\n' "$PASS_COUNT" "$FAIL_COUNT" "$SKIP_COUNT"
  printf '%s\n' "ALL SKIPPED — no sandbox daemon configured"
  exit 0
fi
ok "sandbox daemon reachable at $BASE"

command -v python3 >/dev/null 2>&1 || { printf '  [FAIL] python3 not found\n'; exit 2; }
command -v git >/dev/null 2>&1 || { printf '  [FAIL] git not found\n'; exit 2; }

# ---------------------------------------------------------------------------
# Pick an LLM backend for the worker sessions (cost-gated, like release-smoke.sh)
# ---------------------------------------------------------------------------
DEFAULT_BACKEND="opencode-acp"
if [[ -z "${DW_LLM_BACKEND:-}" && "${DW_MAJOR:-0}" == "1" ]]; then
  DEFAULT_BACKEND="claude-code"
fi
PROBE_BACKEND="$DEFAULT_BACKEND"
[[ "$PROBE_BACKEND" == "opencode-acp" ]] && PROBE_BACKEND="opencode"

backend_available() { # $1 = mode name; echoes yes/no
  curl "${CURL_ARGS[@]}" "$BASE/api/backends" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for b in d.get('llm', []):
    if b.get('name')=='$1':
        print('yes' if b.get('available') else 'no')
        break
else:
    print('no')
" 2>/dev/null || echo "no"
}

if [[ "$(backend_available "$PROBE_BACKEND")" != "yes" && "$DEFAULT_BACKEND" == "opencode-acp" && -z "${DW_LLM_BACKEND:-}" ]]; then
  # Free backend not here — claude-code costs quota, gated behind DW_MAJOR=1
  if [[ "$(backend_available "claude-code")" == "yes" && "${DW_MAJOR:-0}" == "1" ]]; then
    DEFAULT_BACKEND="claude-code"
  else
    skip "no free LLM backend (opencode-acp) on sandbox — automatic-commit smoke skipped"
    printf '         Re-run with DW_MAJOR=1 (allows claude-code, spends quota) or DW_LLM_BACKEND=<name>.\n'
    printf '\n=== RESULTS ===\n'
    printf 'PASS: %d\nFAIL: %d\nSKIP: %d\n' "$PASS_COUNT" "$FAIL_COUNT" "$SKIP_COUNT"
    printf '%s\n' "ALL SKIPPED — no LLM backend available (cost gate closed)"
    exit 0
  fi
fi
ok "worker backend: $DEFAULT_BACKEND"

# ---------------------------------------------------------------------------
# Scratch git repo (fresh)
# ---------------------------------------------------------------------------
REPO=$(mktemp -d /tmp/dw-smoke-autocommit.XXXXXXXX)
REPO_URL=$(python3 -c 'import os; print(os.path.realpath("$REPO"))' 2>/dev/null || echo "$REPO")
SID_A=""
SID_B=""

# Runs on EVERY exit path (normal, failure, INT, TERM, HUP): ends the
# sessions this run started — by their exact recorded ids, and only for
# those ids (the sandbox daemon is external and is never signalled by
# this script) — then removes the scratch repo. Idempotent, so it is
# safe to run once from the signal path and again from the EXIT trap.
cleanup() {
  local rc=$? # first line: $? is still the script's own pending status
  # Ignore further HUP/INT/TERM while cleanup is running: a second Ctrl-C
  # mid-cleanup would `exit` from the signal handler and leave this run's
  # sessions behind — cleanup must be allowed to finish.
  trap '' HUP INT TERM
  for sid in "$SID_A" "$SID_B"; do
    if [[ -n "$sid" ]]; then
      curl "${CURL_ARGS[@]}" -X POST -H "Content-Type: application/json" \
        -d '{"id":"'"$sid"'"}' "$BASE/api/sessions/kill" >/dev/null 2>&1 || true
    fi
  done
  if [[ -z "${KEEP_TEST_DIR:-}" ]]; then
    rm -rf "$REPO" 2>/dev/null || true
  else
    printf '  (KEEP_TEST_DIR set — left scratch repo at %s)\n' "$REPO_URL"
  fi
  exit $rc
}
# SIGINT/SIGTERM/SIGHUP arrive with the default bash action (no EXIT trap
# runs), which would leave this run's sessions on the sandbox daemon and
# the scratch repo on disk. The handlers below exit with the conventional
# 128+signal status and the EXIT trap does the rest.
on_signal() {
  case "$1" in
    HUP)  exit 129 ;;
    INT)  exit 130 ;;
    TERM) exit 143 ;;
  esac
}
trap cleanup EXIT
trap 'on_signal HUP' HUP
trap 'on_signal INT' INT
trap 'on_signal TERM' TERM

git -C "$REPO" init -q -b main
git -C "$REPO" config user.email "smoke@example.com"
git -C "$REPO" config user.name "datawatch-smoke"
# Baseline commit so the repo has history and a clean tree before the worker.
printf 'baseline\n' > "$REPO/README.md"
git -C "$REPO" add .
git -C "$REPO" commit -q -m "initial"
ok "scratch git repo ready at $REPO_URL (1 baseline commit)"

# ---------------------------------------------------------------------------
# Session runner
# ---------------------------------------------------------------------------
# start_and_wait <name> | echoes session id on success
COMMIT_SUBJECT="created hello file"
TASK="Create a single file named hello.txt in the repository root containing only the line 'hello'. If hello.txt already exists with exactly that content and the working tree is already clean, change nothing at all. Do not run any version-control git commands yourself — leave all committing to the daemon. When you are done, print exactly: DATAWATCH_COMPLETE: $COMMIT_SUBJECT"
TIMEOUT="${SMOKE_TIMEOUT_SECONDS:-240}"
# Enforce a usable minimum so the poll loop always runs at least once
# (a sub-5s value would give `seq 1 0` and an instant spurious timeout).
if (( TIMEOUT < 15 )); then
  TIMEOUT=15
fi

start_session() { # $1 = name | echoes full session id (or empty)
  local name="$1" resp
  resp=$(curl "${CURL_ARGS[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"task\":$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$TASK"),
\"project_dir\":\"$REPO_URL\",
\"backend\":\"$DEFAULT_BACKEND\",
\"name\":\"$name\",
\"one_shot\":true,
\"auto_git_commit\":true,
\"auto_git_init\":false}" \
    "$BASE/api/sessions/start" 2>/dev/null)
  echo "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("full_id",""))' 2>/dev/null || echo ""
}

wait_complete() { # $1 = session id | returns 0 if state reached complete
  local sid="$1" i state
  for i in $(seq 1 $((TIMEOUT / 5))); do
    sleep 5
    state=$(curl "${CURL_ARGS[@]}" "$BASE/api/sessions" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin); ss=d.get('sessions') if isinstance(d,dict) else d
for s in (ss or []):
    if s.get('full_id')=='$sid':
        print(s.get('state',''))
        break" 2>/dev/null || echo "")
    if [[ "$state" == "complete" ]]; then
      return 0
    fi
  done
  return 1
}

task_commit_count() { # number of commits whose subject is the worker summary
  git -C "$REPO" log --format=%s | grep -Fxc "$COMMIT_SUBJECT" || true
}

# ===========================================================================
H "(a) one-shot session auto-commits the worker's change with the DATAWATCH_COMPLETE summary as the commit message"
# ===========================================================================
SID_A=$(start_session "smoke-autocommit-a")
if [[ -z "$SID_A" ]]; then
  ko "could not start first one-shot session (backend=$DEFAULT_BACKEND repo=$REPO_URL)"
else
  ok "session started ($SID_A)"
  if wait_complete "$SID_A"; then
    ok "session completed within ${TIMEOUT}s"
  else
    ko "session did not complete within ${TIMEOUT}s"
  fi

  # Fetch the worker's visible output for the did-not-self-commit assertion.
  OUTPUT=$(curl "${CURL_ARGS[@]}" "$BASE/api/output?id=$SID_A&n=20000" 2>/dev/null || echo "")
  # Command-shaped match only (invocation with flag args, e.g. 'git commit
  # -m ...', or line-anchored / prompt-anchored 'git commit', or after a
  # shell separator) — a bare prose mention of the words does not count as
  # an invocation.
  SELF_COMMIT_RE='(^|[;&|][[:space:]]*|[$#>❯][[:space:]])git[[:space:]]+commit([[:space:]]*$|[[:space:]]+-[a-zA-Z-])|git[[:space:]]+commit[[:space:]]+-[a-zA-Z-]'
  if echo "$OUTPUT" | grep -qE "$SELF_COMMIT_RE"; then
    ko "worker ran git commit on its own (daemon auto-commit must own commits)"
  else
    ok "worker output contains no git commit invocation"
  fi

  # (a)-3: exactly one commit whose message is the DATAWATCH_COMPLETE summary.
  COUNT_A=$(task_commit_count)
  SUBJ_A=$(git -C "$REPO" log -1 --format=%s 2>/dev/null || echo "<no commits>")
  if [[ "$COUNT_A" == "1" && "$SUBJ_A" == "$COMMIT_SUBJECT" ]]; then
    ok "repo holds exactly one commit named '$COMMIT_SUBJECT' (git log -1 subject matches)"
  else
    ko "expected exactly one commit with subject '$COMMIT_SUBJECT' (found $COUNT_A; last subject: $SUBJ_A)"
  fi

  # (a)-4: working tree is clean (commit landed, no uncommitted leftovers).
  DIRTY=$(git -C "$REPO" status --porcelain 2>/dev/null || true)
  if [[ -z "$DIRTY" ]]; then
    ok "working tree is clean after auto-commit"
  else
    ko "working tree is dirty after auto-commit: $(echo "$DIRTY" | tr '\n' ' ')"
  fi

  # (a)-5: the file the worker created was actually committed.
  if git -C "$REPO" cat-file -e HEAD:hello.txt 2>/dev/null; then
    ok "hello.txt present in HEAD commit"
  else
    ko "hello.txt missing from HEAD commit"
  fi
fi

# ===========================================================================
H "(b) re-run of the same task on the already-committed repo adds no second commit"
# ===========================================================================
SID_B=$(start_session "smoke-autocommit-b")
if [[ -z "$SID_B" ]]; then
  ko "could not start retry one-shot session (backend=$DEFAULT_BACKEND repo=$REPO_URL)"
else
  ok "retry session started ($SID_B)"
  wait_complete "$SID_B" && ok "retry session completed within ${TIMEOUT}s" \
                      || ko "retry session did not complete within ${TIMEOUT}s"

  # (b)-2: still exactly ONE commit named 'created hello file' — the daemon
  # must skip the completion commit when the worker left no new change.
  COUNT_B=$(task_commit_count)
  TOTAL_B=$(git -C "$REPO" rev-list --count HEAD 2>/dev/null || echo 0)
  if [[ "$COUNT_B" == "1" && "$TOTAL_B" == "2" ]]; then
    ok "repo still holds exactly one task commit (total commits: $TOTAL_B = baseline + 1)"
  elif [[ "$COUNT_B" == "1" ]]; then
    ok "repo still holds exactly one commit named '$COMMIT_SUBJECT' (total commits: $TOTAL_B)"
  else
    ko "expected still exactly 1 commit named '$COMMIT_SUBJECT' (found $COUNT_B; total $TOTAL_B)"
  fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n=== RESULTS ===\n'
printf 'PASS: %d\n' "$PASS_COUNT"
printf 'FAIL: %d\n' "$FAIL_COUNT"
printf 'SKIP: %d\n' "$SKIP_COUNT"

if [[ $FAIL_COUNT -gt 0 ]]; then
  printf '%s\n' "FAILED"
  exit 1
else
  printf '%s\n' "ALL PASSED — automatic completion commits work end-to-end: $(git -C "$REPO" log --oneline 2>/dev/null | tr '\n' ' ' | sed 's/^ *//')"
  exit 0
fi
