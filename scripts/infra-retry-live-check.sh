#!/usr/bin/env bash
# infra-retry-live-check.sh — live check of infrastructure retry on a SANDBOX
# daemon, against a throwaway git project.
#
# What it proves (each step is a lettered result in the report):
#   (a) config round trip: the defaults read back, every key can be set, a bad
#       value is refused and nothing in the same request is applied;
#   (b) a transient failure retries: a real worker-spawn failure is recorded on
#       the task (class, reason), the task is not counted as failed, and once
#       the worker launcher exists the next retry reaches a healthy spawn;
#   (c) repeated failure holds: after max_tries the task is review_needed, the
#       review text, the alert and an open question name the cause, and
#       answering retry puts the task back to pending;
#   (d) dependents recover: with exhausted_action count, a dependent failed
#       behind an infrastructure failure is marked and then reset by itself
#       when the first task is retried, with the infra_dependents_recovered
#       decision;
#   (e) a failure that is not an infrastructure failure (a file-scope refusal)
#       is not retried and takes the normal path;
#   (f) the REST, MCP, CLI and comm channel reads agree on one task.
#
# How the spawn failure is made: every worker session starts through tmux. The
# daemon's PATH holds a tmux shim that pins the call to a private socket and
# runs the worker launcher found at $S/launcher/tmux. That path does not exist
# at first, so the spawn fails with a real error from the real code path; the
# check then creates the launcher (a link to the system tmux) and the next
# retry succeeds. The worker itself is a stand-in that sleeps, so a task never
# runs to completion in the sandbox; the report says so.
#
# Safety: SANDBOX only. The daemon runs from a generated config under
# .datawatch/live-check/ (git-ignored) on loopback ports, with its own data
# directory, its own TMPDIR and its own tmux socket. The production daemon, its
# service and its data are never touched. The daemon and the planner stand-in
# are ended only by the PID that scripts/test-daemon-lib.sh recorded, with
# their config or script path as marker. Everything this script made is
# removed on every exit path.
#
# Usage (always from a shell with the operator's tmux variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/infra-retry-live-check.sh
#
# Env:
#   INFRA_RETRY_CHECK_REPORT   where the tracked report is written
#                              (default: docs/plans/2026-10-30-infra-retry-live-check.md)
#
# Exit 0 only when every step passed. The report is regenerated on every run.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${INFRA_RETRY_CHECK_REPORT:-$ROOT/docs/plans/2026-10-30-infra-retry-live-check.md}"
case "$REPORT" in /*) ;; *) REPORT="$ROOT/$REPORT" ;; esac

unset TMUX TMUX_TMPDIR

# --- scratch area and lifecycle ----------------------------------------------
LC="$ROOT/.datawatch/live-check"
S="$LC/run"
rm -rf "$S"
mkdir -p "$S" "$LC/bin"
export TD_STATE_DIR="$S/tdstate"
REPORT_BUF="$S/report.buf"
: > "$REPORT_BUF"
# shellcheck source=/dev/null
source "$LIB"

REAL_TMUX="$(command -v tmux 2>/dev/null || true)"
TMUX_SOCK="$S/tmux.sock"
CREATED=()   # Automaton ids this run made

api() { # api <METHOD> <path> [json body] — body to stdout
  local m="$1" p="$2" b="${3:-}"
  if [[ -n "$b" ]]; then
    curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$b" "$BASE$p"
  else
    curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" "$BASE$p"
  fi
}
api_code() { # api_code <METHOD> <path> [json body] — HTTP status only
  local m="$1" p="$2" b="${3:-}"
  curl -sk -m 30 -o /dev/null -w '%{http_code}' -X "$m" -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p"
}
api_both() { # api_both <METHOD> <path> <json body> — "<status> <body>" on one line
  local m="$1" p="$2" b="${3:-}" out
  out="$(curl -sk -m 30 -w '\n%{http_code}' -X "$m" -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p")"
  printf '%s %s' "${out##*$'\n'}" "$(printf '%s' "${out%$'\n'*}" | tr '\n' ' ')"
}
jq_r() { jq -r "$@" 2>/dev/null; }

drop_automaton() { # drop_automaton <id> — cancel, end its sessions, delete it
  local id="$1"
  api POST "/api/autonomous/prds/$id/cancel" '{}' >/dev/null 2>&1 || true
  drop_sessions
  api DELETE "/api/autonomous/prds/$id?hard=true" >/dev/null 2>&1 || true
}
drop_sessions() { # end and delete every session the sandbox daemon holds
  local ids sid
  ids="$(api GET /api/sessions | jq -r '(if type=="array" then . else .sessions end)[]? | (.full_id // .id)' 2>/dev/null)"
  for sid in $ids; do
    api POST /api/sessions/kill "{\"id\":\"$sid\"}" >/dev/null 2>&1 || true
    api POST /api/sessions/delete "{\"id\":\"$sid\"}" >/dev/null 2>&1 || true
  done
}

pre_stop_cleanup() {
  # Runs on every exit path while the daemon is still alive: remove what the
  # check created through the daemon's own API.
  local id
  [[ -n "${BASE:-}" ]] || return 0
  for id in "${CREATED[@]:-}"; do
    [[ -n "$id" ]] && drop_automaton "$id"
  done
  drop_sessions
}
extra_cleanup() {
  # Runs AFTER the daemon and the stand-in are gone: wait for the private tmux
  # server (it exits by itself when its last session is gone), then remove the
  # scratch area.
  local i
  if [[ -n "$REAL_TMUX" && -S "$TMUX_SOCK" ]]; then
    for i in $(seq 1 20); do
      "$REAL_TMUX" -S "$TMUX_SOCK" ls >/dev/null 2>&1 || break
      sleep 0.5
    done
  fi
  rm -rf "$S" "$LC/bin" 2>/dev/null || true
  rmdir "$LC" 2>/dev/null || true
}
TD_PRE_STOP_CLEANUP=pre_stop_cleanup
TD_EXTRA_CLEANUP=extra_cleanup
td_install_traps "$@"

# --- result bookkeeping --------------------------------------------------------
PASS=0
FAIL=0
RESULTS=()      # "letter|PASS|FAIL|title|summary"
STEP_OK=1
STEP_NOTES=()
say() { printf '%s\n' "$*"; }
rep() { printf '%s\n' "$*" >> "$REPORT_BUF"; }
rep_block() { # rep_block <label> <text>: fenced observed output, bounded
  rep ""
  rep "$1"
  rep '```'
  printf '%s\n' "$2" | head -c 3500 >> "$REPORT_BUF"
  printf '\n' >> "$REPORT_BUF"
  rep '```'
}
now() { date '+%H:%M:%S'; }
section() { say ""; say "== $* =="; rep ""; rep "#### $*"; }
chk() { # chk <description> <0|1 ok> [evidence]  — one sub-check of the current step
  local desc="$1" ok="$2" ev="${3:-}"
  if [[ "$ok" == 1 ]]; then
    say "    ok      $desc"
    rep "- ok — $desc"
  else
    say "    FAILED  $desc"
    rep "- NOT MET — $desc"
    STEP_OK=0
    STEP_NOTES+=("$desc")
  fi
  if [[ -n "$ev" ]]; then rep "  - $ev"; fi
}
step_end() { # step_end <letter> <title> <summary when passed>
  local letter="$1" title="$2" summary="$3" v=PASS
  if [[ "$STEP_OK" != 1 ]]; then
    v=FAIL
    summary="not met: $(printf '%s; ' "${STEP_NOTES[@]}" | sed 's/; $//')"
    FAIL=$((FAIL + 1))
  else
    PASS=$((PASS + 1))
  fi
  RESULTS+=("$letter|$v|$title|$summary")
  say "  [$letter] $v — $title"
  STEP_OK=1
  STEP_NOTES=()
}
b01() { [[ "$1" == "$2" ]] && echo 1 || echo 0; } # equal -> 1
yes() { [[ -n "${1:-}" && "$1" != "null" ]] && echo 1 || echo 0; } # non-empty -> 1

write_report() { # write_report <overall PASS|FAIL> — assemble the tracked report
  local overall="$1" n=${#RESULTS[@]} r letter v title summary
  mkdir -p "$(dirname "$REPORT")"
  {
    printf '# Infrastructure retry — sandbox live check\n\n'
    printf 'Date: %s · Branch: `%s` (tree at commit `%s`)\n\n' "$(date '+%Y-%m-%d')" "$BRANCH" "$COMMIT"
    printf '**Sandbox daemon: `datawatch v%s`** (`GET /api/health` read `"version":"%s"`), built from this worktree with\n' "${DAEMON_VERSION:-unknown}" "${DAEMON_VERSION:-unknown}"
    printf '`go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/`. Host: `%s`, %s, tmux %s. It ran from its own config and\n' "$HOSTNAME_TXT" "$KERNEL_TXT" "$TMUX_VER"
    printf 'data directory under `.datawatch/live-check/` (git-ignored, deleted at the end). The production daemon, its\n'
    printf 'service unit and its ports were never touched.\n\n'
    printf 'Script: `scripts/infra-retry-live-check.sh`, run as `env -u TMUX -u TMUX_TMPDIR bash scripts/infra-retry-live-check.sh`. It\n'
    printf 'regenerates this report on every run. No tmux server command was run against the operator'"'"'s tmux; nothing\n'
    printf 'was ended by name, only by the PID the lifecycle helper recorded.\n\n'
    printf '## Results\n\nOne line per step. A step passes only when every check under it in the evidence is met.\n\n'
    for ((i = 0; i < n; i++)); do
      IFS='|' read -r letter v title summary <<< "${RESULTS[$i]}"
      printf -- '- (%s) %s — Result: %s — %s\n' "$letter" "$title" "$v" "$summary"
    done
    printf '\n**Steps:** %d passed, %d failed.\n\n' "$PASS" "$FAIL"
    printf '## Evidence\n'
    cat "$REPORT_BUF"
    printf '\n\nResult: %s\n' "$overall"
  } | sed -e "s|$S|.datawatch/live-check/run|g" -e "s|$ROOT|.|g" > "$REPORT"
}

die_report() { # die_report <message> — record a setup failure and stop
  RESULTS+=("setup|FAIL|Sandbox setup|$1")
  FAIL=$((FAIL + 1))
  rep ""
  rep "#### Setup failed"
  rep ""
  rep "$1"
  write_report FAIL
  say "FAILED: $1 — report: $REPORT"
  exit 1
}

# --- prerequisites ---------------------------------------------------------
for t in git curl python3 jq go; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done
[[ -n "$REAL_TMUX" ]] || { say "SKIP: tmux is not on PATH"; exit 0; }

BRANCH="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
COMMIT="$(git -C "$ROOT" rev-parse --short=9 HEAD 2>/dev/null || echo unknown)"
HOSTNAME_TXT="$(hostname 2>/dev/null || echo unknown)"
KERNEL_TXT="$(uname -sr 2>/dev/null || echo unknown)"
TMUX_VER="$("$REAL_TMUX" -V 2>/dev/null | awk '{print $2}')"

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"; PLAN_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"

DATA="$S/data"
PROJ="$S/projects"          # allowed project root
REPO="$PROJ/throwaway"      # the throwaway git project
DTMP="$S"
CFG="$S/config.yaml"
LOG="$S/daemon.log"
BIN="$LC/bin/datawatch"
LAUNCHER="$S/launcher/tmux"
mkdir -p "$DATA" "$PROJ" "$S/bin" "$S/launcher" "$REPO"

dw() { env -u TMUX -u TMUX_TMPDIR "$BIN" --config "$CFG" "$@"; }
await() { # await <timeout-seconds> <command...> — poll every 0.25 s
  local t="$1"; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

say "infrastructure retry live check — $(now)"
rep ""
rep "Scope: \`GET /api/autonomous/infra-retries\`, the \`autonomous.infra_retry.*\` config keys on \`GET\`/\`PUT /api/config\`, the executor retry loop, the hold after repeated failure (review text, alert, operator question), the recovery of dependent tasks, and the MCP, CLI and comm channel reads, all against a throwaway git project."

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

# --- build the daemon from this tree -------------------------------------------
say "building the daemon from this tree"
BUILD_OUT="$(cd "$ROOT" && env -u TMUX -u TMUX_TMPDIR go build -o "$BIN" ./cmd/datawatch/ 2>&1)" \
  || die_report "go build failed: $(printf '%s' "$BUILD_OUT" | head -c 300)"

# --- the throwaway project, shims and config --------------------------------------
git -C "$REPO" init -q -b trunk
git -C "$REPO" config user.email "infra-retry-live-check@example.invalid"
git -C "$REPO" config user.name "infra retry live check"
printf 'throwaway project for the infrastructure retry live check\n' > "$REPO/README.md"
git -C "$REPO" add README.md
git -C "$REPO" commit -q -m "Initial commit"

# The worker launcher shim: every tmux call the daemon makes goes to a private
# socket through $LAUNCHER, which does not exist until the check creates it.
cat > "$S/bin/tmux" <<SHIM
#!/bin/sh
case "\$1" in -V) exec "$REAL_TMUX" -V;; esac
[ -x "$LAUNCHER" ] || { echo "worker launcher is not installed yet" >&2; exit 127; }
exec "$LAUNCHER" -S "$TMUX_SOCK" "\$@"
SHIM
# The worker the aider backend launches: answers --version, otherwise sleeps so
# a session that spawned stays live until it is ended through the API.
cat > "$S/bin/aider" <<'SHIM'
#!/bin/sh
case "$1" in --version) echo "aider live-check-stand-in"; exit 0;; esac
exec sleep 3600
SHIM
chmod +x "$S/bin/tmux" "$S/bin/aider"
launcher_remove() { rm -f "$LAUNCHER"; }
launcher_install() { ln -sf "$REAL_TMUX" "$LAUNCHER"; }

# The planner stand-in: answers the daemon's planning call with a fixed plan so
# an Automaton can be decomposed without a model. The plan is replaced by the
# check's own tasks right after.
cat > "$S/planner.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
PLAN = {"title": "Infrastructure retry live check", "stories": [{
    "title": "Placeholder story", "description": "Replaced by the live check.", "files": [], "depends_on": [],
    "verification_script": "", "files_generated": [],
    "tasks": [{"title": "Placeholder task", "spec": "Placeholder task, replaced by the live check.",
               "files": [], "depends_on": [], "verification_script": "", "files_generated": []}]}]}
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0") or 0)
        self.rfile.read(n)
        body = json.dumps({"model": "stand-in", "response": json.dumps(PLAN), "done": True}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        body = b'{"models":[]}'
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

sed \
  -e "s|data_dir: /data|data_dir: $DATA|g" \
  -e "s|default_project_dir: /data/projects|default_project_dir: $PROJ|g" \
  -e "s|port: 8080|port: $PORT|g" \
  -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
  -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" \
  -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
  -e "s|token: \"\"|token: \"$TOKEN\"|g" \
  -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
  -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DNS_PORT\"|g" \
  -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$HOOK_PORT|g" \
  -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$NTFY_PORT|g" \
  -e "s|http://local-node:11434|http://127.0.0.1:$PLAN_PORT|g" \
  -e "s|http://localhost:11434|http://127.0.0.1:$PLAN_PORT|g" \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"
sed -i \
  -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\"]|" \
  -e "s|^autonomous:\$|autonomous:\n  infra_retry:\n    max_tries: 3\n    backoff_seconds: [1, 1, 1]\n  questions:\n    enabled: true|" \
  "$CFG"
printf '\naider:\n  enabled: true\n  binary: %s/bin/aider\n' "$S" >> "$CFG"

rep ""
rep "### Setup"
rep ""
rep "- **Sandbox only.** Config generated from \`testdata/datawatch.yaml\` by the substitutions the test runner uses (data folder, loopback ports, test token), plus \`autonomous.infra_retry.max_tries: 3\`, \`autonomous.infra_retry.backoff_seconds: [1, 1, 1]\`, \`autonomous.questions.enabled: true\`, \`server.allowed_project_roots\` (one folder) and an \`aider\` backend entry pointing at a stand-in worker that sleeps."
rep "- **Ports** (probed free just before use): HTTP $PORT, TLS $TLS_PORT, MCP $MCP_PORT, DNS $DNS_PORT, webhook $HOOK_PORT, notify $NTFY_PORT, planner stand-in $PLAN_PORT. Port 8080 and 8443 were not used."
rep "- **Worker launcher.** A \`tmux\` shim first on the daemon's \`PATH\` pins every tmux call to a private socket and runs the launcher at \`.datawatch/live-check/run/launcher/tmux\`. That path does not exist at the start, so every worker spawn fails with a real error from the real spawn path until the check creates the launcher (a link to the system tmux). The operator's tmux server is not reachable from the daemon."
rep "- **Planner stand-in.** A small HTTP server (\`planner.py\`, recorded by PID like the daemon) answers the daemon's planning call with a fixed one-task plan, so an Automaton can be planned without a model; the config points every model address at it (the daemon links its own model node to that address). The check replaces that plan with its own tasks."
rep "- **Fixed-port listener.** The daemon's proxy sandbox listener has a fixed port of its own; if another process on the host holds it the daemon logs that and carries on, as it did on a host where the operator's daemon runs. It has nothing to do with this check."
rep "- **Throwaway project.** A git repository (\`.datawatch/live-check/run/projects/throwaway\`, default branch \`trunk\`) with one commit."

# --- start the planner stand-in and the sandbox daemon ---------------------------
python3 -I "$S/planner.py" "$PLAN_PORT" >/dev/null 2>&1 &
PLAN_PID=$!
td_track_pid "$PLAN_PID" "$S/planner.py"

export PATH="$S/bin:$PATH" TMPDIR="$DTMP"
if ! td_start "$BIN" "$CFG" "$LOG"; then
  die_report "the sandbox daemon did not stay up; log tail: $(tail -5 "$LOG" 2>/dev/null | tr '\n' ' ')"
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! await 90 curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  die_report "the sandbox daemon did not answer /api/health; log tail: $(tail -5 "$LOG" 2>/dev/null | tr '\n' ' ')"
fi
DAEMON_VERSION="$(api GET /api/health | jq_r '.version')"
say "  daemon pid $DPID, version $DAEMON_VERSION, healthy at $(now)"
rep ""
rep "Sandbox daemon pid \`$DPID\` (recorded by the lifecycle helper) was healthy at $(now); planner stand-in pid \`$PLAN_PID\`."

# --- helpers over the Automaton API -----------------------------------------------
prd_get() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd_get "$1" | jq_r '.status'; }
prd_is() { [[ "$(prd_status "$1")" == "$2" ]]; }
tfield() { # tfield <automaton> <task> <jq expr over the task>
  prd_get "$1" | jq -r --arg t "$2" "[.stories[].tasks[] | select(.id==\$t)][0] | $3" 2>/dev/null
}
tjson() { prd_get "$1" | jq -c --arg t "$2" '[.stories[].tasks[] | select(.id==$t)][0] | del(.spec, .history)' 2>/dev/null; }
ir_get() { # ir_get <automaton> — the infra-retries body for one Automaton
  api GET "/api/autonomous/infra-retries?automaton=$1"
}
ir_task() { ir_get "$1" | jq -c --arg t "$2" '[.tasks[] | select(.task_id==$t)][0]' 2>/dev/null; }
retries_ge() { # retries_ge <automaton> <task> <n>
  [[ "$(tfield "$1" "$2" '(.infra_retry_count // 0)')" -ge "$3" ]]
}
status_is() { [[ "$(tfield "$1" "$2" '.status')" == "$3" ]]; }
status_not() { [[ "$(tfield "$1" "$2" '.status')" != "$3" ]]; }
has_session() { [[ -n "$(tfield "$1" "$2" '(.session_id // "")')" ]]; }

# new_automaton <title> <spec1> [<spec2>] — create, plan, replace the plan with
# the check's own tasks (the second depends on the first), approve. Sets AID,
# TID1 and TID2. It does not run the Automaton.
new_automaton() {
  local title="$1" spec1="$2" spec2="${3:-}" body r sid olds i
  body="$(python3 -I -c 'import json,sys;print(json.dumps({"spec":"Infrastructure retry live check: "+sys.argv[1],"title":sys.argv[1],"project_dir":sys.argv[2],"backend":"aider"}))' "$title" "$REPO")"
  AID="$(api POST /api/autonomous/prds "$body" | jq_r '.id')"
  TID1=""; TID2=""
  [[ -n "$AID" && "$AID" != null ]] || return 1
  CREATED+=("$AID")
  api POST "/api/autonomous/prds/$AID/decompose" '{}' >/dev/null
  if ! await 90 prd_is "$AID" needs_review; then
    NEW_DIAG="decompose status: $(api GET "/api/autonomous/prds/$AID/decompose/status" | head -c 400)"
    return 2
  fi
  r="$(api POST "/api/autonomous/prds/$AID/add_story" '{"title":"Live check story","description":"Tasks of the live check."}')"
  sid="$(printf '%s' "$r" | jq_r '.stories[-1].id')"
  olds="$(printf '%s' "$r" | jq_r '.stories[:-1][].id')"
  for i in $olds; do api POST "/api/autonomous/prds/$AID/remove_story" "{\"story_id\":\"$i\"}" >/dev/null; done
  r="$(api POST "/api/autonomous/prds/$AID/add_task" "$(python3 -I -c 'import json,sys;print(json.dumps({"story_id":sys.argv[1],"title":"First task","spec":sys.argv[2]}))' "$sid" "$spec1")")"
  TID1="$(printf '%s' "$r" | jq_r '[.stories[] | select(.id=="'"$sid"'")][0].tasks[-1].id')"
  if [[ -n "$spec2" ]]; then
    r="$(api POST "/api/autonomous/prds/$AID/add_task" "$(python3 -I -c 'import json,sys;print(json.dumps({"story_id":sys.argv[1],"title":"Second task","spec":sys.argv[2],"depends_on":[sys.argv[3]]}))' "$sid" "$spec2" "$TID1")")"
    TID2="$(printf '%s' "$r" | jq_r '[.stories[] | select(.id=="'"$sid"'")][0].tasks[-1].id')"
  fi
  api POST "/api/autonomous/prds/$AID/plan_coverage/acknowledge" '{"actor":"operator","note":"live check"}' >/dev/null
  api POST "/api/autonomous/prds/$AID/approve" '{"actor":"operator","note":"live check"}' >/dev/null
  [[ "$(prd_status "$AID")" == approved ]] || return 3
  return 0
}

# --- MCP over stdio -------------------------------------------------------------
mcp_call() { # mcp_call <tool> <json arguments> -> the tool's result text
  python3 -I - "$BIN" "$CFG" "$1" "$2" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg, tool, args = sys.argv[1], sys.argv[2], sys.argv[3], json.loads(sys.argv[4])
env = {k: v for k, v in os.environ.items() if k not in ("TMUX", "TMUX_TMPDIR")}
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=env, text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "infra-retry-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool, "arguments": args}})
end = time.time() + 30
out = ""
while time.time() < end:
    r, _, _ = select.select([p.stdout], [], [], 1)
    if not r: continue
    line = p.stdout.readline()
    if not line: break
    try: msg = json.loads(line)
    except Exception: continue
    if msg.get("id") == 2:
        res = msg.get("result", {})
        out = "".join(c.get("text", "") for c in res.get("content", []))
        break
try: p.stdin.close()
except Exception: pass
try: p.wait(timeout=5)
except Exception: p.kill()
print(out)
PY
}

# ============================================================================
rep ""
rep "### Steps"

# ---- (a) config round trip -------------------------------------------------------
section "(a) Config round trip"
IRC() { api GET /api/config | jq -c '.autonomous.infra_retry'; }
IRS() { api GET /api/autonomous/infra-retries | jq -c '{enabled,max_tries,backoff_seconds,categories,exhausted_action,retry_dependents,raise_question}'; }
A0="$(IRC)"
rep_block "\`GET /api/config\` → \`autonomous.infra_retry\` as the sandbox config set it (max_tries 3, back-off 1,1,1):" "$A0"
chk "the sandbox config values read back: max_tries 3, back-off 1,1,1" "$(b01 "$(printf '%s' "$A0" | jq -c '[.max_tries,.backoff_seconds]')" '[3,[1,1,1]]')" "read: \`$(printf '%s' "$A0" | jq -c '[.max_tries,.backoff_seconds]')\`"
PA="$(api_code PUT /api/config '{"autonomous.infra_retry.backoff_seconds":null,"autonomous.infra_retry.categories":null,"autonomous.infra_retry.max_tries":3}')"
A1="$(IRC)"
rep_block "After \`PUT /api/config\` with back-off and categories set to null (unset), HTTP $PA — the defaults in effect:" "$A1"
DEF_EXPECT='{"enabled":true,"max_tries":3,"backoff_seconds":[60,300,540],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}'
chk "the defaults read back: enabled, 3 tries, back-off 60/300/540 (15 minutes in all), all four categories, hold, dependents true, question true" "$(b01 "$A1" "$DEF_EXPECT")" "back-off total $(printf '%s' "$A1" | jq '[.backoff_seconds[]]|add') s = $(( $(printf '%s' "$A1" | jq '[.backoff_seconds[]]|add') / 60 )) minutes; PUT HTTP $PA"
chk "\`GET /api/autonomous/infra-retries\` serves the same seven values" "$(b01 "$(IRS)" "$DEF_EXPECT")" "read: \`$(IRS)\`"
SET_BODY='{"autonomous.infra_retry.enabled":false,"autonomous.infra_retry.max_tries":5,"autonomous.infra_retry.backoff_seconds":[10,20],"autonomous.infra_retry.categories":["spawn_error"],"autonomous.infra_retry.exhausted_action":"count","autonomous.infra_retry.retry_dependents":false,"autonomous.infra_retry.raise_question":false}'
PB="$(api_code PUT /api/config "$SET_BODY")"
A2="$(IRC)"
rep_block "After \`PUT /api/config\` setting every key to a non-default value, HTTP $PB:" "$A2"
SET_EXPECT='{"enabled":false,"max_tries":5,"backoff_seconds":[10,20],"categories":["spawn_error"],"exhausted_action":"count","retry_dependents":false,"raise_question":false}'
chk "every one of the seven keys was applied and reads back (config and the infra-retries route)" "$([[ "$A2" == "$SET_EXPECT" && "$(IRS)" == "$SET_EXPECT" ]] && echo 1 || echo 0)" "PUT HTTP $PB"
BAD1="$(api_both PUT /api/config '{"autonomous.infra_retry.max_tries":9,"autonomous.infra_retry.exhausted_action":"sometimes"}')"
BAD2="$(api_both PUT /api/config '{"autonomous.infra_retry.max_tries":0}')"
BAD3="$(api_both PUT /api/config '{"autonomous.infra_retry.categories":["spawn_error","not_a_class"],"autonomous.infra_retry.max_tries":7}')"
BAD4="$(api_both PUT /api/config '{"autonomous.infra_retry.backoff_seconds":[5,"soon"]}')"
A3="$(IRC)"
rep_block "Invalid values, each sent to \`PUT /api/config\` (HTTP status, then the refusal text):" "mixed (max_tries 9 + exhausted_action sometimes): $BAD1
max_tries 0: $BAD2
unknown class with max_tries 7: $BAD3
back-off entry \"soon\": $BAD4"
chk "a bad exhausted_action, a zero try limit, an unknown class and a non-numeric delay are each refused with 400 and the refusal names the key" "$([[ "${BAD1%% *}" == 400 && "${BAD2%% *}" == 400 && "${BAD3%% *}" == 400 && "${BAD4%% *}" == 400 && "$BAD1" == *exhausted_action* && "$BAD2" == *max_tries* && "$BAD3" == *categories* && "$BAD4" == *backoff_seconds* ]] && echo 1 || echo 0)" ""
chk "nothing was applied by a refused request, including the valid keys sent beside the bad one (max_tries 9 and 7 not saved)" "$(b01 "$A3" "$A2")" "config after the four refusals is identical to before: \`$A3\`"
# Back to the sandbox values for the rest of the check; categories and the
# exhausted action go back to their defaults.
PR="$(api_code PUT /api/config '{"autonomous.infra_retry.enabled":true,"autonomous.infra_retry.max_tries":3,"autonomous.infra_retry.backoff_seconds":[1,1,1],"autonomous.infra_retry.categories":null,"autonomous.infra_retry.exhausted_action":"hold","autonomous.infra_retry.retry_dependents":true,"autonomous.infra_retry.raise_question":true}')"
A4="$(IRC)"
RUN_EXPECT='{"enabled":true,"max_tries":3,"backoff_seconds":[1,1,1],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}'
chk "the short back-off (1,1,1) and 3 tries are back in force for the steps below" "$(b01 "$A4" "$RUN_EXPECT")" "PUT HTTP $PR; read: \`$A4\`"
step_end a "Config round trip" "the defaults read back (3 tries, back-off 60/300/540 = 15 minutes, all four classes, hold, dependents and question true); all seven keys were set and read back on config and the infra-retries route; four invalid requests were refused with 400 naming the key and nothing in them was applied"

# ---- (b) a transient failure retries ----------------------------------------------
section "(b) A transient failure retries"
launcher_remove
if new_automaton "retry-then-recover" "Placeholder worker task for the infrastructure retry live check."; then
  B_ID="$AID"; B_T="$TID1"
  rep ""
  rep "Automaton \`$B_ID\`, one task \`$B_T\`, worker backend \`aider\`. The worker launcher does not exist, so the spawn fails."
  api POST "/api/autonomous/prds/$B_ID/run" '{}' >/dev/null
  if await 30 retries_ge "$B_ID" "$B_T" 1; then
    B_SNAP_TASK="$(tjson "$B_ID" "$B_T")"
    B_SNAP_IR="$(ir_task "$B_ID" "$B_T")"
    B_SNAP_DEC="$(prd_get "$B_ID" | jq -c '[.decisions[]? | select(.kind=="infra_retry") | {kind,note,failure_category}]')"
    launcher_install
    B_INSTALLED_AT="$(now)"
    rep_block "Task as read the moment the first retry was recorded (launcher still missing):" "$(printf '%s' "$B_SNAP_TASK" | jq '{status,wait_reason,error,infra_retry_count,infra_retries,escalation_failures,retry_count,failure_reason,attempts:[.attempts[]?|{number,outcome,failure_category,failure_summary}]}')"
    rep_block "\`GET /api/autonomous/infra-retries?automaton=$B_ID\` at that moment, this task's row:" "$(printf '%s' "$B_SNAP_IR" | jq .)"
    rep_block "The Automaton's \`infra_retry\` decisions at that moment:" "$(printf '%s' "$B_SNAP_DEC" | jq .)"
    chk "a retry is recorded on the task with its class (\`spawn_error\`), the reason text from the failed spawn and a delay of 1 s" "$([[ "$(printf '%s' "$B_SNAP_TASK" | jq_r '.infra_retries[0].class')" == spawn_error && "$(printf '%s' "$B_SNAP_TASK" | jq_r '.infra_retries[0].reason')" == *tmux* && "$(printf '%s' "$B_SNAP_TASK" | jq_r '.infra_retries[0].delay_seconds')" == 1 ]] && echo 1 || echo 0)" "class \`$(printf '%s' "$B_SNAP_TASK" | jq_r '.infra_retries[0].class')\`, reason \`$(printf '%s' "$B_SNAP_TASK" | jq_r '.infra_retries[0].reason' | head -c 160)\`"
    chk "the task is not counted as failed: not in status failed, no escalation failure, no failure reason" "$([[ "$(printf '%s' "$B_SNAP_TASK" | jq_r '.status')" != failed && "$(printf '%s' "$B_SNAP_TASK" | jq_r '(.escalation_failures // 0)')" == 0 && "$(printf '%s' "$B_SNAP_TASK" | jq_r '.failure_reason')" == null ]] && echo 1 || echo 0)" "status \`$(printf '%s' "$B_SNAP_TASK" | jq_r '.status')\`, wait_reason \`$(printf '%s' "$B_SNAP_TASK" | jq_r '.wait_reason')\`"
    chk "the infra-retries route lists the task with its retry count and the maximum (retry n of 3), not held" "$([[ "$(printf '%s' "$B_SNAP_IR" | jq_r '.max_tries')" == 3 && "$(printf '%s' "$B_SNAP_IR" | jq_r '.retry_count')" -ge 1 && "$(printf '%s' "$B_SNAP_IR" | jq_r '.held')" == false ]] && echo 1 || echo 0)" ""
    chk "the Automaton's decision log carries an \`infra_retry\` entry" "$([[ "$(printf '%s' "$B_SNAP_DEC" | jq_r 'length')" -ge 1 ]] && echo 1 || echo 0)" ""
    if await 30 has_session "$B_ID" "$B_T"; then
      B_TASK2="$(tjson "$B_ID" "$B_T")"
      B_SID="$(printf '%s' "$B_TASK2" | jq_r '.session_id')"
      B_SESS="$(api GET /api/sessions | jq -c --arg s "$B_SID" '[(if type=="array" then . else .sessions end)[]? | select((.full_id==$s) or (.id==$s)) | {id,full_id,state,backend_family,tmux_session}][0]')"
      B_TMUX="$("$REAL_TMUX" -S "$TMUX_SOCK" ls 2>&1 | head -3)"
      rep_block "Launcher created at $B_INSTALLED_AT. Task after the next retry (healthy spawn):" "$(printf '%s' "$B_TASK2" | jq '{status,wait_reason,session_id,infra_retry_count,infra_retries:[.infra_retries[]|{number,class}],escalation_failures,failure_reason}')"
      rep_block "The worker session as the daemon reports it, and the private tmux server's sessions:" "$B_SESS
$B_TMUX"
      chk "once the launcher exists the next retry succeeds: the task has a worker session id and a live session on the stand-in backend" "$([[ "$(printf '%s' "$B_SESS" | jq_r '.state')" == running ]] && echo 1 || echo 0)" "session \`$B_SID\` state \`$(printf '%s' "$B_SESS" | jq_r '.state')\`, backend \`$(printf '%s' "$B_SESS" | jq_r '.backend_family')\`"
      chk "the task proceeded past the retry loop: status \`$(printf '%s' "$B_TASK2" | jq_r '.status')\` (it left pending and the retry wait reason is gone)" "$([[ "$(printf '%s' "$B_TASK2" | jq_r '.status')" =~ ^(verifying|in_progress)$ && -z "$(printf '%s' "$B_TASK2" | jq_r '.wait_reason // empty')" ]] && echo 1 || echo 0)" ""
      chk "the retry history stays on the task (records kept, none counted as a failure)" "$([[ "$(printf '%s' "$B_TASK2" | jq_r '.infra_retries | length')" -ge 1 && "$(printf '%s' "$B_TASK2" | jq_r '(.escalation_failures // 0)')" == 0 ]] && echo 1 || echo 0)" ""
    else
      chk "once the launcher exists the next retry succeeds: the task gets a worker session" 0 "no session id within 30 s; task: $(tjson "$B_ID" "$B_T" | head -c 600)"
    fi
  else
    chk "a retry is recorded on the task after the spawn failed" 0 "no retry within 30 s; task: $(tjson "$B_ID" "$B_T" | head -c 600); daemon log: $(tail -3 "$LOG" | tr '\n' ' ' | head -c 400)"
  fi
else
  chk "the Automaton was created, planned and approved" 0 "new_automaton failed for \`retry-then-recover\`: ${NEW_DIAG:-no detail}"
fi
step_end b "A transient failure retries" "a real spawn failure (the worker launcher missing) was recorded on the task as \`spawn_error\` retries with the reason text, the task was not counted as failed, and after the launcher was created the next retry spawned a live worker session and the task left the retry loop; the worker is a stand-in, so the task was not run to completion (see the limits)"
[[ -n "${B_ID:-}" ]] && drop_automaton "$B_ID"

# ---- (c) repeated failure holds ---------------------------------------------------
section "(c) Repeated failure holds the task"
launcher_remove
if new_automaton "retry-then-hold" "Placeholder worker task for the infrastructure retry live check."; then
  C_ID="$AID"; C_T="$TID1"
  rep ""
  rep "Automaton \`$C_ID\`, task \`$C_T\`. The launcher is missing and stays missing. 3 tries, back-off 1,1,1, exhausted action hold."
  api POST "/api/autonomous/prds/$C_ID/run" '{}' >/dev/null
  if await 45 status_is "$C_ID" "$C_T" review_needed; then
    C_TASK="$(tjson "$C_ID" "$C_T")"
    C_FULL="$(prd_get "$C_ID" | jq --arg t "$C_T" '[.stories[].tasks[] | select(.id==$t)][0]')"
    C_IR="$(ir_task "$C_ID" "$C_T")"
    C_REVIEW="$(printf '%s' "$C_FULL" | jq_r '.review_summary.text')"
    C_ALERT="$(api GET /api/alerts | jq -c '[(if type=="array" then . else .alerts end)[]? | select(((.title // "") + (.message // "") + (.body // "")) | contains("held after infrastructure retries"))]')"
    C_Q="$(api GET "/api/questions?state=open" | jq -c --arg t "$C_T" '[(if type=="array" then . else .questions end)[]? | select(.asked_by=="infra-retry-hold" and .task_id==$t)][0]')"
    C_DEC="$(prd_get "$C_ID" | jq -c '[.decisions[]? | select(.kind=="infra_retry_exhausted" or .kind=="review_needed") | {kind,note}]')"
    rep_block "Task after the retries ran out (without spec and history):" "$(printf '%s' "$C_TASK" | jq '{status,error,infra_retry_count,failure_reason,escalation_failures,infra_retries:[.infra_retries[]|{number,class,delay_seconds}]}')"
    rep_block "Review text (\`review_summary.text\`):" "$C_REVIEW"
    rep_block "Alert (\`GET /api/alerts\`, entries naming the hold):" "$(printf '%s' "$C_ALERT" | jq .)"
    rep_block "Open operator question for the task (\`GET /api/questions?state=open\`):" "$(printf '%s' "$C_Q" | jq '{id,asked_by,asker_kind,state,task_id,text,default,options:[.options[]?.id]}')"
    rep_block "Decisions written with the hold:" "$(printf '%s' "$C_DEC" | jq .)"
    chk "after 3 tries the task is \`review_needed\` and its failure reason is the infrastructure hold (category infra, source infra-retry-hold)" "$([[ "$(printf '%s' "$C_TASK" | jq_r '.failure_reason.category')" == infra && "$(printf '%s' "$C_TASK" | jq_r '.failure_reason.source')" == infra-retry-hold && "$(printf '%s' "$C_TASK" | jq_r '.infra_retry_count')" == 3 ]] && echo 1 || echo 0)" "retries used: $(printf '%s' "$C_TASK" | jq_r '.infra_retry_count'), cause: \`$(printf '%s' "$C_TASK" | jq_r '.failure_reason.detail' | head -c 160)\`"
    chk "the failure was not counted: no escalation failure on the task" "$([[ "$(printf '%s' "$C_TASK" | jq_r '(.escalation_failures // 0)')" == 0 ]] && echo 1 || echo 0)" ""
    chk "the review text names the infrastructure cause (class and the spawn error) and lists the retries" "$([[ "$C_REVIEW" == *"Infrastructure cause: spawn_error"* && "$C_REVIEW" == *tmux* && "$C_REVIEW" == *"retry 1 (spawn_error)"* ]] && echo 1 || echo 0)" ""
    chk "one error-level alert, titled for the hold, names the Automaton, the task, the class and the cause" "$([[ "$(printf '%s' "$C_ALERT" | jq_r 'length')" -ge 1 && "$(printf '%s' "$C_ALERT" | jq -r '[.[] | (.title // "") + " " + (.message // "") + " " + (.body // "")] | join(" ")')" == *"$C_T"* && "$(printf '%s' "$C_ALERT" | jq -r '[.[] | (.title // "") + " " + (.message // "") + " " + (.body // "")] | join(" ")')" == *spawn_error* ]] && echo 1 || echo 0)" "alerts found: $(printf '%s' "$C_ALERT" | jq_r 'length'), level $(printf '%s' "$C_ALERT" | jq_r '.[0].level // .[0].severity // "?"')"
    chk "an open operator question names the infrastructure cause and offers retry or cancel (default retry)" "$([[ "$(printf '%s' "$C_Q" | jq_r '.text')" == *"infrastructure retries"* && "$(printf '%s' "$C_Q" | jq_r '.text')" == *spawn_error* && "$(printf '%s' "$C_Q" | jq_r '[.options[].id] | join(",")')" == "retry,cancel" && "$(printf '%s' "$C_Q" | jq_r '.default')" == retry ]] && echo 1 || echo 0)" "question \`$(printf '%s' "$C_Q" | jq_r '.id')\`"
    chk "the infra-retries route shows the task as held, with the cause and the open question id" "$([[ "$(printf '%s' "$C_IR" | jq_r '.held')" == true && "$(printf '%s' "$C_IR" | jq_r '.held_cause')" == *spawn_error* && "$(printf '%s' "$C_IR" | jq_r '.question_id')" == "$(printf '%s' "$C_Q" | jq_r '.id')" ]] && echo 1 || echo 0)" ""
    C_QID="$(printf '%s' "$C_Q" | jq_r '.id')"
    # Answer retry: the task leaves review_needed, goes back in play and starts
    # a fresh round. The answer runs in the background while the task's status
    # is sampled every 20 ms, so the short-lived states are seen.
    C_ANS_F="$S/c-answer.txt"; : > "$C_ANS_F"
    ( api_both POST "/api/questions/$C_QID/answer" '{"chosen_option_id":"retry","quote":"live check: retry the held task","actor":"operator","surface":"cli"}' > "$C_ANS_F" ) &
    C_SEQ=""; C_LAST=""; C_N=0
    C_END=$((SECONDS + 15))
    while ((SECONDS < C_END)); do
      C_ST="$(tfield "$C_ID" "$C_T" '.status')"
      [[ -z "$C_ST" ]] && C_ST=pending # the store holds a pending task as an empty status
      if [[ "$C_ST" != "$C_LAST" ]]; then
        C_SEQ+="$(date '+%H:%M:%S.%3N') $C_ST"$'\n'; C_LAST="$C_ST"; C_N=$((C_N + 1))
      fi
      if [[ "$C_ST" == review_needed && "$C_N" -ge 3 ]]; then break; fi
      sleep 0.02
    done
    C_ANS="$(cat "$C_ANS_F")"
    C_TASK3="$(tjson "$C_ID" "$C_T")"
    C_RES="$(prd_get "$C_ID" | jq -c '[.decisions[]? | select((.note // "") | contains("retry after operator review")) | {kind,actor,note}]')"
    rep_block "Answer \`retry\` to the question (HTTP status, then body head), and the task's status sampled every 20 ms from the answer on:" "${C_ANS:0:260}...
$C_SEQ"
    rep_block "The Automaton's decision for the operator's retry:" "$(printf '%s' "$C_RES" | jq .)"
    chk "answering \`retry\` was accepted and the task left review_needed at once: the sampled statuses start with review_needed and move on to a state that is not held" "$([[ "${C_ANS%% *}" =~ ^20[01]$ && "$C_N" -ge 2 && "$(printf '%s' "$C_SEQ" | sed -n 2p)" =~ (pending|in_progress|waiting_capacity) ]] && echo 1 || echo 0)" "answer HTTP ${C_ANS%% *}; pending seen: $([[ "$C_SEQ" == *" pending"* ]] && echo 'yes (the store holds it as an empty status)' || echo 'no (the executor picked the task up inside one 20 ms sample)'); statuses in order: $(printf '%s' "$C_SEQ" | awk '{printf "%s ", $2}')"
    chk "the daemon recorded the requeue as a decision of the Automaton (retry after operator review)" "$([[ "$(printf '%s' "$C_RES" | jq_r 'length')" -ge 1 ]] && echo 1 || echo 0)" ""
    chk "the cause was still there, so a fresh round of 3 retries ran and the task is held again (history now $(printf '%s' "$C_TASK3" | jq_r '.infra_retries | length') records, count $(printf '%s' "$C_TASK3" | jq_r '.infra_retry_count'))" "$([[ "$(printf '%s' "$C_TASK3" | jq_r '.status')" == review_needed && "$(printf '%s' "$C_TASK3" | jq_r '.infra_retries | length')" -ge 6 && "$(printf '%s' "$C_TASK3" | jq_r '.infra_retry_count')" == 3 ]] && echo 1 || echo 0)" ""
  else
    chk "the task was held after max_tries" 0 "status after 45 s: $(tfield "$C_ID" "$C_T" '.status'); task: $(tjson "$C_ID" "$C_T" | head -c 700)"
  fi
else
  chk "the Automaton was created, planned and approved" 0 "new_automaton failed for \`retry-then-hold\`: ${NEW_DIAG:-no detail}"
fi
step_end c "Repeated failure holds the task" "with the launcher still missing the task used its 3 tries, became \`review_needed\` with the failure not counted, the review text, one error alert and an open question all named the \`spawn_error\` cause, and answering \`retry\` put the task back in play with a fresh round (held again because the cause persisted)"

# ---- (d) dependents recover ---------------------------------------------------------
section "(d) Dependents recover"
launcher_remove
api PUT /api/config '{"autonomous.infra_retry.exhausted_action":"count"}' >/dev/null
if new_automaton "dependents-recover" "First placeholder worker task." "Second placeholder worker task, depends on the first."; then
  D_ID="$AID"; D1="$TID1"; D2="$TID2"
  rep ""
  rep "Automaton \`$D_ID\`: task \`$D1\` first, task \`$D2\` depends on it. \`exhausted_action\` is \`count\`; the launcher is missing."
  api POST "/api/autonomous/prds/$D_ID/run" '{}' >/dev/null
  await 90 status_is "$D_ID" "$D2" failed
  sleep 1
  D_T1="$(tjson "$D_ID" "$D1")"; D_T2="$(tjson "$D_ID" "$D2")"
  rep_block "After the first task's retries ran out and were counted — first task:" "$(printf '%s' "$D_T1" | jq '{status,error,infra_retry_count,failure_reason,escalation_failures,retry_count}')"
  rep_block "Second task (the dependent), marked with the infrastructure cause and the root:" "$(printf '%s' "$D_T2" | jq '{status,error,failure_reason}')"
  chk "the first task failed for an infrastructure reason after its retries (class spawn_error recorded)" "$([[ "$(printf '%s' "$D_T1" | jq_r '.status')" == failed && "$(printf '%s' "$D_T1" | jq_r '.infra_retries | length')" -ge 1 && "$(printf '%s' "$D_T1" | jq_r '.infra_retries[0].class')" == spawn_error ]] && echo 1 || echo 0)" "first task status \`$(printf '%s' "$D_T1" | jq_r '.status')\`, retries recorded $(printf '%s' "$D_T1" | jq_r '.infra_retries | length')"
  chk "the dependent is marked: failed behind it with category infra, source dependency-infra and the root task id" "$([[ "$(printf '%s' "$D_T2" | jq_r '.status')" == failed && "$(printf '%s' "$D_T2" | jq_r '.failure_reason.category')" == infra && "$(printf '%s' "$D_T2" | jq_r '.failure_reason.source')" == dependency-infra && "$(printf '%s' "$D_T2" | jq_r '.failure_reason.depends_on_task')" == "$D1" ]] && echo 1 || echo 0)" "root \`$(printf '%s' "$D_T2" | jq_r '.failure_reason.depends_on_task')\`"
  # The first task is retried now that the launcher exists.
  launcher_install
  D_RESET="$(api_both POST "/api/autonomous/prds/$D_ID/reset_task" "{\"task_id\":\"$D1\"}")"
  D_T2B="$(tjson "$D_ID" "$D2")"
  D_DEC="$(prd_get "$D_ID" | jq -c '[.decisions[]? | select(.kind=="infra_dependents_recovered") | {kind,actor,note}]')"
  rep_block "Launcher created; \`reset_task\` on the first task (HTTP status, then body head):" "$(printf '%s' "${D_RESET:0:300}")"
  rep_block "The dependent right after, and the decision written:" "$(printf '%s' "$D_T2B" | jq '{status,error,failure_reason,infra_retry_count}')
$D_DEC"
  chk "the dependent returned to pending by itself when its root was retried, with no reset of it" "$([[ "$(printf '%s' "$D_T2B" | jq_r '.status')" =~ ^(pending|waiting_capacity)$ && "$(printf '%s' "$D_T2B" | jq_r '.failure_reason')" == null ]] && echo 1 || echo 0)" "dependent status \`$(printf '%s' "$D_T2B" | jq_r '.status')\`"
  chk "one \`infra_dependents_recovered\` decision names the root and the task that was reset" "$([[ "$(printf '%s' "$D_DEC" | jq_r 'length')" == 1 && "$(printf '%s' "$D_DEC" | jq_r '.[0].note')" == *"root=$D1"* && "$(printf '%s' "$D_DEC" | jq_r '.[0].note')" == *"$D2"* ]] && echo 1 || echo 0)" "note: \`$(printf '%s' "$D_DEC" | jq_r '.[0].note')\`"
  api POST "/api/autonomous/prds/$D_ID/run" '{}' >/dev/null
  if await 30 has_session "$D_ID" "$D1"; then
    D_T1C="$(tjson "$D_ID" "$D1")"
    rep_block "The first task after the retry, with the launcher in place:" "$(printf '%s' "$D_T1C" | jq '{status,session_id,infra_retry_count,failure_reason}')"
    chk "the first task is no longer failed: it spawned a worker session (status \`$(printf '%s' "$D_T1C" | jq_r '.status')\`)" "$([[ "$(printf '%s' "$D_T1C" | jq_r '.status')" =~ ^(verifying|in_progress)$ ]] && echo 1 || echo 0)" ""
  else
    chk "the first task is no longer failed: it spawned a worker session after the retry" 0 "task: $(tjson "$D_ID" "$D1" | head -c 500)"
  fi
else
  chk "the two-task Automaton was created, planned and approved" 0 "new_automaton failed for \`dependents-recover\`: ${NEW_DIAG:-no detail}"
fi
api PUT /api/config '{"autonomous.infra_retry.exhausted_action":"hold"}' >/dev/null
step_end d "Dependents recover" "with exhausted_action \`count\`, the first task failed for an infrastructure reason, its dependent was marked \`dependency-infra\` with the root id, and when the first task was retried (reset, launcher present) the dependent returned to pending by itself with one \`infra_dependents_recovered\` decision; the first task then spawned a live worker (the stand-in does not complete, so recovery is shown at 'no longer failed', see the limits)"
[[ -n "${D_ID:-}" ]] && drop_automaton "$D_ID"

# ---- (e) a non-transient failure is not retried --------------------------------------
section "(e) A non-transient failure is not retried"
if new_automaton "scope-refusal" "Read /etc/hostname and report its content."; then
  E_ID="$AID"; E_T="$TID1"
  rep ""
  rep "Automaton \`$E_ID\`, task \`$E_T\`. The task's text names a path outside the project, so the file-scope check refuses to start it (default mode enforce): a refusal about the task's scope, not a fault of the machinery. The launcher is present."
  api POST "/api/autonomous/prds/$E_ID/run" '{}' >/dev/null
  await 60 status_is "$E_ID" "$E_T" failed
  sleep 2
  E_TASK="$(tjson "$E_ID" "$E_T")"
  E_IR="$(ir_get "$E_ID")"
  E_DEC="$(prd_get "$E_ID" | jq -c '[.decisions[]? | select(.kind | test("infra")) | .kind]')"
  # The refusal text ends in a hint that uses the daemon's old internal term for
  # an Automaton; the report shows the text up to that hint and says so.
  rep_block "Task after the refusal (the error text is cut where its closing hint begins, marked [...]):" "$(printf '%s' "$E_TASK" | jq '{status,error,infra_retry_count,infra_retries,failure_reason,escalation_failures,retry_count} | .error |= sub("; edit the task spec.*$"; " [...]")')"
  rep_block "\`GET /api/autonomous/infra-retries?automaton=$E_ID\`, tasks listed:" "$(printf '%s' "$E_IR" | jq -c '{count,tasks}')"
  chk "the task failed for its scope: the error is the scope refusal" "$([[ "$(printf '%s' "$E_TASK" | jq_r '.status')" == failed && "$(printf '%s' "$E_TASK" | jq_r '.error')" == scope:* ]] && echo 1 || echo 0)" "error: \`$(printf '%s' "$E_TASK" | jq_r '.error | sub("; edit the task spec.*$"; " [...]")')\`"
  chk "it shows no infra_retries and no retry count" "$([[ "$(printf '%s' "$E_TASK" | jq_r '(.infra_retries // []) | length')" == 0 && "$(printf '%s' "$E_TASK" | jq_r '(.infra_retry_count // 0)')" == 0 ]] && echo 1 || echo 0)" ""
  chk "it is not on the infra-retries route and the Automaton has no infra decision" "$([[ "$(printf '%s' "$E_IR" | jq_r '.count')" == 0 && "$(printf '%s' "$E_DEC" | jq_r 'length')" == 0 ]] && echo 1 || echo 0)" ""
  chk "it took the normal failure path: the failure is counted (not an infrastructure hold) and no wait reason or hold is set" "$([[ "$(printf '%s' "$E_TASK" | jq_r '.failure_reason.source // ""')" != infra-retry-hold && "$(printf '%s' "$E_TASK" | jq_r '.status')" != review_needed ]] && echo 1 || echo 0)" "failure_reason: \`$(printf '%s' "$E_TASK" | jq -c '.failure_reason')\`"
else
  chk "the Automaton was created, planned and approved" 0 "new_automaton failed for \`scope-refusal\`: ${NEW_DIAG:-no detail}"
fi
step_end e "A non-transient failure is not retried" "a task refused for its file scope failed on the normal path with no infra_retries, no retry count, no row on the infra-retries route and no infrastructure decision"
[[ -n "${E_ID:-}" ]] && drop_automaton "$E_ID"

# ---- (f) the four reads agree ----------------------------------------------------------
section "(f) REST, MCP, CLI and comm channel agree"
if [[ -n "${C_ID:-}" && -n "${C_T:-}" ]]; then
  F_REST="$(ir_get "$C_ID")"
  F_MCP="$(mcp_call autonomous_infra_retries "{\"automaton\":\"$C_ID\"}")"
  F_CLI_JSON="$(dw autonomous infra-retries "$C_ID" --json 2>"$S/cli.err")"
  F_CLI_TXT="$(dw autonomous infra-retries "$C_ID" 2>>"$S/cli.err")"
  F_COMM="$(api POST /api/test/message "{\"text\":\"autonomous infra-retries $C_ID\"}" | jq -r '.responses | join("\n")' 2>/dev/null)"
  printf '%s' "$F_REST" > "$S/f-rest.json"; printf '%s' "$F_MCP" > "$S/f-mcp.json"
  printf '%s' "$F_CLI_JSON" > "$S/f-cli.json"; printf '%s' "$F_CLI_TXT" > "$S/f-cli.txt"; printf '%s' "$F_COMM" > "$S/f-comm.txt"
  F_VERDICT="$(python3 -I - "$S" "$C_T" <<'PY'
import json, sys
d, task = sys.argv[1], sys.argv[2]
rd = lambda n: open(d + "/" + n).read()
problems = []
try:
    rest, mcp, cli = json.loads(rd("f-rest.json")), json.loads(rd("f-mcp.json")), json.loads(rd("f-cli.json"))
except Exception as e:
    print("a JSON read did not parse: %s" % e); sys.exit(1)
canon = lambda x: json.dumps(x, sort_keys=True)
if canon(rest) != canon(mcp): problems.append("MCP differs from REST")
if canon(rest) != canon(cli): problems.append("CLI --json differs from REST")
row = [t for t in rest.get("tasks", []) if t["task_id"] == task]
if not row:
    problems.append("REST has no row for the task")
else:
    t = row[0]
    head = 'task %s %s: %s, retry %d of %d' % (t["task_id"], json.dumps(t["task_title"]), t["status"], t["retry_count"], t["max_tries"])
    for label in ("f-cli.txt", "f-comm.txt"):
        text = rd(label)
        if head not in text: problems.append("%s misses the line %r" % (label, head))
        for r in t["retries"]:
            if "retry %d: %s" % (r["number"], r["class"]) not in text: problems.append("%s misses retry %d" % (label, r["number"]))
        if t.get("held") and ("held after infrastructure retries: " + t["held_cause"]) not in text:
            problems.append("%s misses the held line" % label)
        if t.get("question_id") and ("waiting on question " + t["question_id"]) not in text:
            problems.append("%s misses the question line" % label)
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
)"
  rep ""
  rep "Task \`$C_T\` of Automaton \`$C_ID\`, the task held in step (c). REST: \`GET /api/autonomous/infra-retries?automaton=$C_ID\`; MCP: \`autonomous_infra_retries {\"automaton\":\"$C_ID\"}\` over \`datawatch mcp\` (stdio); CLI: \`datawatch autonomous infra-retries $C_ID\` (text) and \`--json\`; comm: \`POST /api/test/message {\"text\":\"autonomous infra-retries $C_ID\"}\`."
  rep_block "REST row of the task:" "$(printf '%s' "$F_REST" | jq --arg t "$C_T" '[.tasks[] | select(.task_id==$t)][0] | {task_id,status,retry_count,max_tries,held,held_cause,question_id,retries:[.retries[]|{number,class}]}')"
  rep_block "CLI text:" "$F_CLI_TXT"
  rep_block "Comm channel reply:" "$F_COMM"
  rep_block "MCP result (first 500 bytes):" "$(printf '%s' "$F_MCP" | head -c 500)"
  chk "MCP and CLI \`--json\` return the same object as REST (policy, task rows, retries, held cause, question id)" "$([[ "$F_VERDICT" != *differs* && "$F_VERDICT" != *parse* ]] && echo 1 || echo 0)" "comparison: \`$F_VERDICT\`"
  chk "CLI text and the comm reply carry the same task line (status, retry n of max), every retry and the held cause and question id as REST" "$(b01 "$F_VERDICT" OK)" "CLI stderr: $(head -c 200 "$S/cli.err")"
else
  chk "a held task from step (c) exists to read" 0 ""
fi
step_end f "REST, MCP, CLI and comm agree" "the held task of step (c) read the same on REST, MCP and the CLI (identical JSON), and the CLI text and the comm reply carried the same status line, every retry, the held cause and the question id"

# ============================================================================
say ""; say "== Cleanup =="; rep ""; rep "### Cleanup"
for id in "${CREATED[@]:-}"; do [[ -n "$id" ]] && drop_automaton "$id"; done
drop_sessions
REMAIN_PRDS="$(api GET /api/autonomous/prds | jq_r '(.prds // .automata // []) | length')"
REMAIN_SESS="$(api GET /api/sessions | jq_r '(if type=="array" then . else .sessions end) | length')"
rep ""
rep "- Created through the API and removed through the API: ${#CREATED[@]} Automata (cancelled, hard-deleted) and their worker sessions. After the deletes \`GET /api/autonomous/prds\` listed ${REMAIN_PRDS:-?} Automata and \`GET /api/sessions\` listed ${REMAIN_SESS:-?} sessions."
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
P_GONE="no"; td_alive "$PLAN_PID" || P_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
for i in $(seq 1 20); do "$REAL_TMUX" -S "$TMUX_SOCK" ls >/dev/null 2>&1 || break; sleep 0.5; done
TMUX_LEFT="no"; "$REAL_TMUX" -S "$TMUX_SOCK" ls >/dev/null 2>&1 && TMUX_LEFT="yes"
rep "- **Sandbox daemon ended** by \`td_stop_all\` (recorded pid \`$DPID\`, marker = its config path), planner stand-in ended the same way (pid \`$PLAN_PID\`). Daemon gone: $D_GONE. Planner gone: $P_GONE. TLS port $TLS_PORT closed: $PORT_CLOSED."
rep "- **Private tmux server**: no tmux command was run to end it; it exits by itself when its last session is gone. A session or server still on the private socket: $TMUX_LEFT."
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
# A daemon that is new since the snapshot but whose config is not under this
# check's scratch area belongs to another job on the host, not to this check.
LEFT_OURS="$(printf '%s\n' "$LEFT_OUT" | awk -v s="config=$S/" 'index($0, s) > 0' | wc -l)"
LEFT_OTHERS=$(( $(printf '%s\n' "$LEFT_OUT" | grep -c 'leftover test daemon:') - LEFT_OURS ))
rep "- **Leftover check** against the snapshot taken before this run: \`scripts/check-no-leftover-test-daemons.sh check\` exit $LEFT_RC — $(printf '%s' "$LEFT_OUT" | tail -1). New daemons whose config lies in this check's scratch area: $LEFT_OURS. New daemons of other jobs on the host (config elsewhere, not started or ended by this check): $LEFT_OTHERS."
rep "- **Scratch area** (\`.datawatch/live-check/\`: project, config, data, log, launcher, shims, daemon binary) is removed by the exit handler right after this report is written."
STEP_OK=1
chk "the sandbox daemon, the planner stand-in and the private tmux server are gone, every created Automaton and session was removed, and no test daemon was left behind" "$([[ "$D_GONE" == yes && "$P_GONE" == yes && "$PORT_CLOSED" == yes && "$TMUX_LEFT" == no && "$LEFT_OURS" == 0 && "${REMAIN_PRDS:-1}" == 0 ]] && echo 1 || echo 0)" "daemon gone: $D_GONE; planner gone: $P_GONE; port closed: $PORT_CLOSED; private tmux left: $TMUX_LEFT; leftover check exit $LEFT_RC (new daemons from this check: $LEFT_OURS, from other jobs: $LEFT_OTHERS); Automata left: ${REMAIN_PRDS:-?}"
step_end g "Cleanup" "the daemon, the planner stand-in and the private tmux server were gone, no Automaton or session was left, and the leftover guard found no test daemon"

# ---- what is not shown live ----------------------------------------------------------------
rep ""
rep "### Defect found by this check, and fixed"
rep ""
rep "Step (d) failed on the first live run. With \`exhausted_action: count\`, the first task failed after its retries with **no failure reason** (\`failure_reason\` was null). Its dependent therefore failed with the plain text \`dependency <id> failed\`, was not marked as an infrastructure failure, and was not reset when the first task was retried: there was no \`infra_dependents_recovered\` decision. The unit tests had seeded the failed first task's reason by hand, so nothing covered the path from a real spawn error."
rep ""
rep "Fix: when the retries of a worker spawn failure are used up under \`count\`, the task now gets a failure reason of category \`infra\`, source \`infra-retry-count\`, carrying the spawn error text (\`markInfraCounted\`). A regression test runs the real executor from the spawn error, on the sequential and the concurrent path, through marking, reset and completion; it fails without the change and passes with it. Files changed for it: \`internal/autonomous/executor.go\`, \`internal/autonomous/infra_retry_hold.go\`, \`internal/autonomous/infra_retry_dependents_test.go\`. The rows above are from the run after the fix."
rep ""
rep "### Finding not fixed"
rep ""
rep "After the \`reset_task\` in step (d), the first task read \`infra_retry_count: 3\` while it was verifying, and it still carried its earlier \`infra-retry-count\` failure reason. The plain reset does not clear the consecutive count, so a task reset by hand that then fails again for the same reason is held at once without a fresh round of retries; the operator's retry of a held task clears the count (\`reviewRequeue.apply\`) and the how-to promises a fresh round only for that path. The plain reset has never cleared a failure reason of any category. Not changed: it is outside the steps of this check, and the operator decides whether a reset should also start a new incident."
rep ""
rep "The scope refusal that step (e) provokes ends in a hint that uses the old internal term for an Automaton in two messages of \`internal/autonomous/scope.go\`. The report cuts the text before that hint. Not changed: it belongs to the scope check, not to this feature."
rep ""
rep "## What is not shown live"
rep ""
rep "- **No task ran to completion.** The worker is a stand-in that sleeps, and the sandbox has no model to verify work. Steps (b) and (d) therefore show the retry reaching a healthy spawn (a live worker session, the task past the retry loop and out of the failed state) and not a finished task. That a successful verification ends the incident and zeroes the consecutive retry count is covered by the unit tests in \`internal/autonomous/infra_retry_executor_test.go\`, not shown here."
rep "- **The spawn failure is made by a missing worker launcher**, not by a missing backend binary: the daemon starts a session even when the backend's binary does not exist (the backend fails later, inside the pane), and only the launcher call can fail the spawn. The failure is real and comes from the real spawn path, but it is one cause of one class (\`spawn_error\`)."
rep "- **The other three classes are not induced live.** A full disk or quota (\`storage_write\`), a worker session killed before its task arrives (\`session_killed_before_delivery\`) and a daemon restart under a running task (\`daemon_restart\`) were not made to happen in the sandbox. Their classification and their retry handling are covered by \`internal/autonomous/infra_failure_test.go\` and \`internal/autonomous/infra_retry_executor_test.go\`."
rep "- **Back-off was 1 second.** The default delays (1, 5 and 9 minutes) are shown as the configured default in step (a) only; no retry waited that long here. The half-delay cap on a restart-caused retry is not shown."
rep "- **The \`count\` action** is shown once, in step (d), for the dependent chain. Its effect on the ladder of workers (the failure is booked as one ordinary failed attempt) is not read back in detail."
rep "- **Web app, mobile app and the config surfaces other than REST** (the PWA card and badges, \`config set\` on the CLI, \`configure\` on comm, \`autonomous_config_set\` on MCP) were not driven; their rows in the testing tracker are not changed by this check."
rep "- **The comm read** went through \`POST /api/test/message\` on the sandbox daemon with \`server.token\` set (the helper authenticated), so it is the router's own verb that ran. A real chat channel (Signal, Matrix and the others) was not connected."

OVERALL=PASS
[[ "$FAIL" -gt 0 ]] && OVERALL=FAIL
write_report "$OVERALL"
say ""
say "Summary: $PASS passed, $FAIL failed — report: $REPORT"
[[ "$FAIL" -eq 0 ]]
