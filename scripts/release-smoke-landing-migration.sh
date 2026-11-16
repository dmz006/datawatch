#!/usr/bin/env bash
# release-smoke-landing-migration.sh — live smoke for the one-time landing
# migration at daemon start.
#
# Starts its OWN sandbox daemon (fresh scratch data dir, free loopback port,
# plain HTTP, no token) on a data dir seeded with two archived Automaton
# records and a real git base repository:
#   - "unmerged": archived, branch automaton/smoke-unmerged with a commit that
#     is not on main  -> the migration must mark it landing=pending;
#   - "merged":   archived, branch automaton/smoke-merged fast-forwarded into
#     main (no commit ahead) -> the migration must leave it without a landing
#     record.
#
# What it proves, against the built binary:
#   (a) the daemon log (stderr / <data_dir>/daemon-crash.log) carries the
#       migration's count line (marked 1);
#   (b) GET /api/autonomous/prds/<unmerged> returns landing.state=pending with
#       actor landing_migration, status still archived;
#   (c) GET /api/autonomous/prds/<merged> has no landing record;
#   (d) GET /api/alerts holds the "Archived Automata with unlanded branches"
#       alert naming the count;
#   (e) the done marker landing_migration.done is written in the data dir;
#   (f) after a restart on the same data dir the migration does not run again
#       (no second alert, no second count line).
#
# SAFETY: never touches the production daemon or ~/.datawatch. The daemon is
# started and ended through scripts/test-daemon-lib.sh (only the recorded PID,
# only while its config path is in its cmdline). It runs no tmux command and
# starts no session. Scratch lives under the repository (never /tmp) and is
# removed on exit unless KEEP_TEST_DIR=1.
#
# Usage:
#   scripts/release-smoke-landing-migration.sh
#
# Env vars:
#   DATAWATCH_BIN   datawatch binary (default: ./datawatch, else bin/datawatch)
#   KEEP_TEST_DIR   set to 1 to keep the scratch folder for inspection
#
# Exit codes:
#   0 — all checks passed (or cleanly skipped: no binary / missing tooling)
#   1 — one or more checks failed
set -uo pipefail
unset TMUX TMUX_TMPDIR

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
SCRATCH_ROOT="$ROOT/.smoke-scratch"
S="" # this run's scratch folder, made after the traps are installed

# Lifecycle: the shared library's traps (EXIT, INT, TERM) end only the daemon
# this script records; TD_EXTRA_CLEANUP then removes the scratch folder.
# The state dir lives under the repository (never /tmp) and survives the
# library's one-time re-exec through the environment.
mkdir -p "$SCRATCH_ROOT"
export TD_STATE_DIR="${TD_STATE_DIR:-$SCRATCH_ROOT/td-state.$$}"
smoke_extra_cleanup() {
  if [[ "${KEEP_TEST_DIR:-0}" == "1" && -n "$S" ]]; then
    echo "kept: $S"
  else
    [[ -n "$S" ]] && rm -rf "$S"
  fi
  rm -rf "$TD_STATE_DIR"
  rmdir "$SCRATCH_ROOT" 2>/dev/null
  return 0
}
TD_EXTRA_CLEANUP=smoke_extra_cleanup
# shellcheck source=scripts/test-daemon-lib.sh
source "$LIB"
td_install_traps "$@"

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS_COUNT=$((PASS_COUNT + 1)); }
ko()   { printf '  [FAIL] %s\n' "$1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
skip() { printf '  [SKIP] %s\n' "$1"; SKIP_COUNT=$((SKIP_COUNT + 1)); }
summary() {
  printf '\n=== RESULTS ===\nPASS: %d\nFAIL: %d\nSKIP: %d\n' "$PASS_COUNT" "$FAIL_COUNT" "$SKIP_COUNT"
}

for tool in curl python3 git; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    skip "$tool not found — landing-migration smoke skipped"; summary; exit 0
  fi
done
BIN="${DATAWATCH_BIN:-}"
if [[ -z "$BIN" ]]; then
  if [[ -x "$ROOT/datawatch" ]]; then BIN="$ROOT/datawatch"
  elif [[ -x "$ROOT/bin/datawatch" ]]; then BIN="$ROOT/bin/datawatch"
  fi
fi
if [[ -z "$BIN" || ! -x "$BIN" ]]; then
  skip "no datawatch binary (build it or set DATAWATCH_BIN)"; summary; exit 0
fi

S="$(mktemp -d "$SCRATCH_ROOT/landing-migration.XXXXXX")"

free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'; }

# --- base repository: main, one merged branch, one unmerged branch ---------
REPO="$S/repo"
G() { git -C "$REPO" -c user.name=smoke -c user.email=smoke@example.invalid "$@" >/dev/null 2>&1; }
mkdir -p "$REPO"
git init -q -b main "$REPO" >/dev/null 2>&1 || { git init -q "$REPO" && G checkout -b main; }
echo base > "$REPO/README"; G add README; G commit -m base
G checkout -b automaton/smoke-merged; echo merged > "$REPO/merged"; G add merged; G commit -m merged
G checkout main; G merge --ff-only automaton/smoke-merged
G checkout -b automaton/smoke-unmerged; echo unmerged > "$REPO/unmerged"; G add unmerged; G commit -m unmerged
G checkout main

# --- seed data dir with two archived Automaton records (legacy layout) -----
DATA="$S/data"
mkdir -p "$DATA/autonomous"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 - "$DATA/autonomous/prds.jsonl" "$REPO" "$NOW" <<'PY'
import json, sys
path, repo, now = sys.argv[1:]
rows = [
    {"id": "5a0e0001", "title": "smoke unmerged", "spec": "smoke", "project_dir": repo,
     "status": "archived", "git": {"branch": "automaton/smoke-unmerged"},
     "created_at": now, "updated_at": now},
    {"id": "5a0e0002", "title": "smoke merged", "spec": "smoke", "project_dir": repo,
     "status": "archived", "git": {"branch": "automaton/smoke-merged"},
     "created_at": now, "updated_at": now},
]
with open(path, "w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")
PY

PORT="$(free_port)"
CFG="$S/config.yaml"
cat > "$CFG" <<YAML
data_dir: ${DATA}
hostname: dw-smoke-landing
server:
  enabled: true
  host: 127.0.0.1
  port: ${PORT}
  tls_enabled: false
  token: ""
memory:
  enabled: false
autonomous:
  enabled: true
  worktree_base_repo: ${REPO}
YAML
BASE="http://127.0.0.1:${PORT}"
LOG="$S/daemon.log"

start_daemon() {
  if ! td_start "$BIN" "$CFG" "$LOG"; then
    ko "daemon start failed"; tail -n 20 "$LOG" >&2; return 1
  fi
  local i
  for i in $(seq 1 60); do
    curl -s --max-time 2 "$BASE/api/health" >/dev/null 2>&1 && return 0
    sleep 0.5
  done
  ko "daemon did not answer /api/health"; tail -n 20 "$LOG" >&2; return 1
}
# wait_for <seconds> <cmd...> — poll until the command succeeds.
wait_for() { local n="$1" i; shift; for i in $(seq 1 $((n * 2))); do "$@" && return 0; sleep 0.5; done; return 1; }
# The daemon's log lines go to stderr, which it also duplicates into
# <data_dir>/daemon-crash.log; read both.
LOGS=("$LOG" "$DATA/daemon-crash.log")
count_lines() { cat "${LOGS[@]}" 2>/dev/null | grep -c 'landing migration: marked' || true; }
has_count_line() { cat "${LOGS[@]}" 2>/dev/null | grep -q 'landing migration: marked 1 archived Automata pending'; }
has_marker() { [[ -f "$DATA/autonomous/landing_migration.done" ]]; }
alert_count() {
  curl -s --max-time 5 "$BASE/api/alerts" | python3 -c '
import json,sys
try: d=json.load(sys.stdin)
except Exception: print(0); sys.exit()
items=d.get("alerts", d) if isinstance(d, dict) else d
print(sum(1 for a in (items or []) if isinstance(a, dict) and a.get("title")=="Archived Automata with unlanded branches" and "marked 1 archived" in a.get("body","")))'
}
prd_field() { # <id> <python expr over p>
  curl -s --max-time 5 "$BASE/api/autonomous/prds/$1" | python3 -c "
import json,sys
try: p=json.load(sys.stdin)
except Exception: print('ERR'); sys.exit()
p=p.get('prd', p)
print($2)"
}

printf '\n=== landing migration at daemon start ===\n'
start_daemon || { summary; exit 1; }

if wait_for 30 has_count_line; then ok "(a) daemon log reports the migration count (marked 1)"; else ko "(a) no 'marked 1' count line in the daemon log"; fi

got="$(prd_field 5a0e0001 '(p.get("status"), (p.get("landing") or {}).get("state"), (p.get("landing") or {}).get("actor"))')"
if [[ "$got" == "('archived', 'pending', 'landing_migration')" ]]; then ok "(b) unmerged archived Automaton marked pending, status unchanged"; else ko "(b) unmerged record = $got"; fi

got="$(prd_field 5a0e0002 '(p.get("status"), p.get("landing"))')"
if [[ "$got" == "('archived', None)" ]]; then ok "(c) merged archived Automaton left without a landing record"; else ko "(c) merged record = $got"; fi

if [[ "$(alert_count)" == "1" ]]; then ok "(d) one operator alert names the count"; else ko "(d) alert count = $(alert_count)"; fi

if wait_for 10 has_marker; then ok "(e) done marker written"; else ko "(e) landing_migration.done missing"; fi

printf '\n=== restart on the same data dir ===\n'
td_stop_all >/dev/null 2>&1 || true
before="$(count_lines)"
start_daemon || { summary; exit 1; }
sleep 3
after="$(count_lines)"
if [[ "$before" == "$after" && "$(alert_count)" == "1" ]]; then
  ok "(f) migration did not run again after restart (count lines $before, one alert)"
else
  ko "(f) after restart: count lines $before -> $after, alerts $(alert_count)"
fi

summary
[[ "$FAIL_COUNT" -eq 0 ]]
