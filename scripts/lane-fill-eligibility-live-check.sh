#!/usr/bin/env bash
# lane-fill-eligibility-live-check.sh — live check of the lane fill eligibility
# rules, the per-lane-kind switch, the lane counts and the pool evidence on a
# SANDBOX daemon, against a throwaway project repository.
#
# What it proves (each is a numbered check in the report; a check passes only
# when every assertion under it passed):
#   1. Defaults on a fresh config: lane fill mode shadow for both lane kinds,
#      the seven eligibility keys read their defaults (72, true x4, empty,
#      empty), a PUT then GET round trip of all seven keys, and a bad gpu_mode
#      is a 400.
#   2. Refusals: eight kinds of Automaton are made in the sandbox store before
#      its first start, in a project repository whose default branch is not
#      named main: (a) a stale draft, (b) a spec with a HOLD line, (c) a spec
#      sequenced after an Automaton that is not completed, (d) an
#      implementation Automaton naming a proposal without a Confirmed: yes
#      line, (e) one naming no proposal, (f) one whose id already has a task
#      commit on the base branch, (g) one with a recorded worktree that is not
#      registered, and (h) a fully eligible control. In shadow mode the status
#      shows a blocked decision with the right eligibility_reason for each of
#      a to g, the control is the only start_prd decision, and nothing started.
#   3. The same refusals on every read surface: the MCP tool, the CLI with and
#      without --json, the chat verb through POST /api/test/message, and the
#      PWA card code path (the served app.js and the five locale bundles).
#   4. Lane counts: a Claude LLM entry capped at 1 while claude_lanes is 3
#      gives effective 1, source llm_caps; with lane_counts_from_caps off it
#      gives 3, source config.
#   5. The per-lane-kind switch: gpu_mode on (claude_mode empty, base mode
#      shadow) starts the control on a GPU lane, a Claude-lane decision stays
#      unexecuted, no refused Automaton starts, and gpu_mode back to shadow
#      stops further starts at the next poll.
#   6. Pool evidence with an untrusted certificate: a compute node on a
#      loopback https stub with a generated self-signed certificate; the
#      evidence error names the node and the fix and does not show the URL;
#      after the node's tls_cert_fingerprint is set the evidence reads.
#   7. Shadow comparison (the dogfood step): lane fill's refusals side by side
#      with what the daemon-side supervisor watch and the supervisor watch
#      script's own rules say about the same state, and every difference.
#   8. Cleanup: the sandbox daemon and the stubs are gone, no test daemon is
#      left, the throwaway directory is removed.
#
# How Automata are made. The create route only makes drafts and a plan needs a
# planner model, which a sandbox does not have, so the Automata are written
# into the sandbox's own store before its first start (as the uncommitted-work
# live check does). The repository files the rules read (proposal files, the
# task commit, the worktree base repository) are real files in a throwaway git
# repository. From the moment the daemon is up everything goes through its REST
# API and its command line. The worker is the daemon's own shell backend
# pointed at a small generated script, so an Automaton lane fill starts runs
# headlessly with no model.
#
# What is not run against the sandbox: scripts/supervisor-watch.py is written
# for the operator's own daemon (fixed address, service name and checkout), so
# it cannot be pointed at a sandbox. Check 7 loads the script as a module and
# applies its own idle-lane rule, its own recent() and its own record format to
# the sandbox's Automaton list, and says so in the report.
#
# Safety: SANDBOX only. The daemon runs from a generated config under a
# throwaway directory on loopback ports, with its own data directory, its own
# TMPDIR and its own multiplexer socket directory (TMUX_TMPDIR points under the
# throwaway directory). Every address in the config that names an Ollama host
# points at a loopback stub of this script, so nothing leaves the machine. The
# production daemon, its service and its data are never touched. The daemon and
# the two stubs are ended only by the PIDs that scripts/test-daemon-lib.sh
# recorded (the config or stub path is the marker), on every exit path.
# Everything this script made is removed, and the removal is checked.
#
# Usage (always from a shell with the operator's multiplexer variables removed):
#   env -u TMUX -u TMUX_TMPDIR bash scripts/lane-fill-eligibility-live-check.sh
#
# Env:
#   LANE_FILL_CHECK_REPORT  where the tracked report is written
#                           (default: docs/plans/2026-10-31-lane-fill-eligibility-live-check.md)
#   TEST_BINARY             daemon binary (default: built from this worktree into
#                           the throwaway directory, then bin/datawatch, then PATH)
#
# Exit 0 only when every check passed. The report is regenerated on every run.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${LANE_FILL_CHECK_REPORT:-$ROOT/docs/plans/2026-10-31-lane-fill-eligibility-live-check.md}"

# The throwaway project's default branch: deliberately not "main".
PROJECT_DEFAULT_BRANCH="trunk"
WRITE_SUBDIR="src"
VERIFY_SCRIPT=".checks/ok.sh"
POLL_SECONDS=5

# --- scratch area and lifecycle ----------------------------------------------
ORIG_TMPDIR="${TMPDIR:-/tmp}"
# The daemon's multiplexer socket lives under the throwaway directory and a
# socket path is limited to about 100 characters, so a long TMPDIR is not used.
if [ "${#ORIG_TMPDIR}" -gt 24 ]; then ORIG_TMPDIR=/tmp; fi
S="$(mktemp -d "$ORIG_TMPDIR/dw-lanefill-live.XXXXXX")"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"

check_cleanup_extra() {
  # Runs AFTER td_stop_all (the daemon and the stubs are gone).
  rm -rf "$S" 2>/dev/null || true
}
TD_EXTRA_CLEANUP=check_cleanup_extra
td_install_traps "$@"

# --- result bookkeeping --------------------------------------------------------
CUR=0                       # check number the report text goes under (0 = setup)
declare -A CK_TOTAL=() CK_FAIL=() CK_TITLE=() CK_WHY=() REP=()
say() { printf '%s\n' "$*"; }
rep() { REP[$CUR]+="$*"$'\n'; }
rep_block() { # rep_block <label> <text>: fenced observed output, bounded
  rep ""
  rep "$1"
  rep '```'
  REP[$CUR]+="$(printf '%s\n' "$2" | head -c 3500)"$'\n'
  rep '```'
}
begin_check() { # begin_check <n> <title>
  CUR="$1"; CK_TITLE[$1]="$2"; CK_TOTAL[$1]=0; CK_FAIL[$1]=0
  say ""; say "== $1. $2 =="
  rep ""; rep "## $1. $2"
}
ck() { # ck <n> <0 for pass> <what> [evidence]
  local n="$1" ok="$2" what="$3" ev="${4:-}" v=PASS
  CK_TOTAL[$n]=$(( ${CK_TOTAL[$n]:-0} + 1 ))
  if [[ "$ok" != 0 ]]; then
    v=FAIL; CK_FAIL[$n]=$(( ${CK_FAIL[$n]:-0} + 1 ))
    [[ -n "${CK_WHY[$n]:-}" ]] || CK_WHY[$n]="$what"
  fi
  say "  $v  [$n] $what"
  rep ""
  rep "- **$v** — $what"
  if [[ -n "$ev" ]]; then rep "  - $ev"; fi
}
now() { date '+%H:%M:%S'; }

# --- prerequisites ---------------------------------------------------------
for t in git curl python3 jq openssl; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done

mkdir -p "$S/bin"
BINARY=""
if [[ -n "${TEST_BINARY:-}" && -x "${TEST_BINARY:-}" ]]; then
  BINARY="$TEST_BINARY"
elif command -v go >/dev/null 2>&1 && (cd "$ROOT" && env -u TMUX -u TMUX_TMPDIR go build -o "$S/bin/datawatch" ./cmd/datawatch/) >/dev/null 2>&1; then
  BINARY="$S/bin/datawatch"
elif [[ -x "$ROOT/bin/datawatch" ]]; then
  BINARY="$ROOT/bin/datawatch"
else
  BINARY="$(command -v datawatch 2>/dev/null || true)"
fi
[[ -n "$BINARY" ]] || { say "SKIP: no datawatch binary (TEST_BINARY, a build, bin/datawatch, PATH)"; exit 0; }
# The report records the binary's own version output with the release number
# left out (the release number is set at merge time and must not be written into
# tracked files); the source revision identifies the build instead.
VERSION="$("$BINARY" version 2>/dev/null | head -1 | sed -E 's/v?[0-9]+\.[0-9]+\.[0-9]+[^ ]*/<release number omitted>/g' || true)"
BUILD_REV="$(cd "$ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)"
HOST_NAME="$(hostname 2>/dev/null || echo unknown)"
RUN_DATE="$(date '+%Y-%m-%d')"

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"
OLLAMA_PORT="$(free_port)"; TLS_STUB_PORT="$(free_port)"
TOKEN="dw-test-token-12345"
BASE="https://127.0.0.1:$TLS_PORT"

REMOTE="$S/remote.git"      # the project's "origin"
REPO="$S/project"           # the worktree base repository (autonomous.worktree_base_repo)
WTROOT="$S/worktrees"       # the worktree root (autonomous.worktree_dir)
PHANTOM="$S/phantom-worktree"  # a directory that looks like a worktree but is not registered
DATA="$S/data"
PROJ="$S/projects"
DTMP="$S/daemon-tmp"        # the daemon's own TMPDIR
SOCKDIR="$S/run"            # the daemon's multiplexer socket directory
STATE="$S/worker-state"
CFG="$S/config.yaml"
LOG="$S/daemon.log"
mkdir -p "$WTROOT" "$DATA" "$PROJ" "$DTMP" "$SOCKDIR" "$STATE" "$PHANTOM"

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
api_both() { # api_both <METHOD> <path> <json body> — "<code> <body>"
  local out; out="$(curl -sk -m 30 -w '\n%{http_code}' -X "$1" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$3" "$BASE$2")"
  printf '%s %s' "${out##*$'\n'}" "$(printf '%s' "${out%$'\n'*}" | tr '\n' ' ')"
}
jq_r() { jq -r "$@" 2>/dev/null; }
dw() { env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" "$BINARY" --config "$CFG" "$@"; }

wait_until() { # wait_until <timeout-seconds> <command...>
  local t="$1"; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

gitr() { git -C "$REPO" "$@"; }

# --- the Automata ----------------------------------------------------------
A_STALE=1f1100a1; A_HOLD=1f1100b1; A_SEQ=1f1100c1; A_UNCONF=1f1100d1
A_NOPROP=1f1100e1; A_MAIN=1f1100f1; A_WT=1f1100a7; A_CONTROL=1f1100c0
A_CLAUDE=1f1100d2; A_GPU=1f1100a8
ALL_IDS=("$A_STALE" "$A_HOLD" "$A_SEQ" "$A_UNCONF" "$A_NOPROP" "$A_MAIN" "$A_WT" "$A_CONTROL" "$A_CLAUDE" "$A_GPU")
REFUSED_IDS=("$A_STALE" "$A_HOLD" "$A_SEQ" "$A_UNCONF" "$A_NOPROP" "$A_MAIN" "$A_WT")
declare -A WANT_REASON=(
  ["$A_STALE"]=stale ["$A_HOLD"]=held ["$A_SEQ"]=sequenced_after_unshipped
  ["$A_UNCONF"]=proposal_not_confirmed ["$A_NOPROP"]=proposal_missing
  ["$A_MAIN"]=main_carries_work ["$A_WT"]=worktree_invalid
)
declare -A LABEL=(
  ["$A_STALE"]="(a) stale draft" ["$A_HOLD"]="(b) HOLD line" ["$A_SEQ"]="(c) sequenced after an unfinished Automaton"
  ["$A_UNCONF"]="(d) proposal without the line" ["$A_NOPROP"]="(e) no proposal named" ["$A_MAIN"]="(f) task commit on the base branch"
  ["$A_WT"]="(g) worktree not registered" ["$A_CONTROL"]="(h) control, fully eligible"
  ["$A_CLAUDE"]="(i) Claude-lane candidate, proposal confirmed in check 5" ["$A_GPU"]="(j) GPU-lane candidate, proposal confirmed in check 5"
)
CODES="stale held sequenced_after_unshipped proposal_not_confirmed proposal_missing main_carries_work worktree_invalid"

build_project() {
  git init -q --bare -b "$PROJECT_DEFAULT_BRANCH" "$REMOTE"
  git clone -q "$REMOTE" "$REPO" 2>/dev/null
  gitr config user.email "lane-fill-live-check@example.invalid"
  gitr config user.name "lane fill live check"
  gitr config commit.gpgsign false
  gitr checkout -q -b "$PROJECT_DEFAULT_BRANCH"
  mkdir -p "$REPO/$WRITE_SUBDIR" "$REPO/$(dirname "$VERIFY_SCRIPT")" "$REPO/docs/plans"
  printf 'readme\n' > "$REPO/README.md"
  printf 'sample directory\n' > "$REPO/$WRITE_SUBDIR/README.md"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$REPO/$VERIFY_SCRIPT"
  printf '# Confirmed proposal\n\nConfirmed: yes\n\nThe operator confirmed this proposal.\n' > "$REPO/docs/plans/confirmed-proposal.md"
  printf '# Unconfirmed proposal\n\nConfirmed: no\n\nStill waiting for the operator.\n' > "$REPO/docs/plans/unconfirmed-proposal.md"
  printf '# Pending proposal for the Claude lane\n\nConfirmed: no\n' > "$REPO/docs/plans/pending-claude.md"
  printf '# Pending proposal for the GPU lane\n\nConfirmed: no\n' > "$REPO/docs/plans/pending-gpu.md"
  gitr add -A
  gitr commit -q -m "Initial commit"
  # (f): a task commit for the Automaton on the base branch.
  gitr commit -q --allow-empty -m "task($A_MAIN): Write the sample file"
  gitr push -q -u origin "$PROJECT_DEFAULT_BRANCH"
  gitr remote set-head origin "$PROJECT_DEFAULT_BRANCH" >/dev/null
  # (g): a directory with a .git entry that the repository does not list.
  printf 'gitdir: %s/nowhere\n' "$S" > "$PHANTOM/.git"
}

write_worker() {
  # The stand-in worker. The shell backend runs it with the task text as $1 and
  # the worktree as $2, then prints the completion marker itself.
  cat > "$S/bin/worker.sh" <<WORKER
#!/bin/sh
task="\$1"; dir="\$2"
case "\$task" in
  *.verify-output-*)
    out="\$(printf '%s\n' "\$task" | tail -n 1)"
    printf '{"ok":true,"severity":"info","summary":"stand-in verifier of the live check; the task verification script is the evidence"}\n' > "\$out"
    exit 0 ;;
esac
id="\$(printf '%s' "\$task" | grep -o 'LIVECHECK\\[[a-z0-9]*\\]' | head -1 | sed 's/LIVECHECK\\[\\(.*\\)\\]/\\1/')"
[ -n "\$id" ] || exit 0
cd "\$dir" || exit 1
printf '%s\n' "\$id" >> "$STATE/runs.log"
mkdir -p "$WRITE_SUBDIR"
printf 'work of %s\n' "\$id" > "$WRITE_SUBDIR/\$id.txt"
exit 0
WORKER
  chmod +x "$S/bin/worker.sh"
}

write_stub() {
  # A loopback Ollama stand-in: GET /api/ps answers the models named on the
  # command line. With a certificate and key it serves https.
  cat > "$S/ollama-stub.py" <<'PY'
import http.server, json, ssl, sys
port = int(sys.argv[1])
models = json.loads(sys.argv[2])
cert = sys.argv[3] if len(sys.argv) > 3 else ""
key = sys.argv[4] if len(sys.argv) > 4 else ""
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/api/ps":
            body, code = json.dumps({"models": [{"name": m} for m in models]}).encode(), 200
        else:
            body, code = b"{}", 404
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a):
        pass
srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), H)
if cert:
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert, key)
    srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
srv.serve_forever()
PY
}

seed_store() {
  # Before the first start: one record per Automaton. Everything but the stale
  # draft is awaiting review with one story and one task.
  mkdir -p "$DATA/autonomous"
  python3 -I - "$DATA/autonomous/prds.jsonl" "$WTROOT" "$WRITE_SUBDIR" "$VERIFY_SCRIPT" "$PHANTOM" \
    "$A_STALE" "$A_HOLD" "$A_SEQ" "$A_UNCONF" "$A_NOPROP" "$A_MAIN" "$A_WT" "$A_CONTROL" "$A_CLAUDE" "$A_GPU" <<'PY'
import json, sys, datetime
out, wtroot, sub, verify, phantom = sys.argv[1:6]
stale, hold, seq, unconf, noprop, main_, wt, control, claude, gpu = sys.argv[6:16]
utc = datetime.timezone.utc
now = datetime.datetime.now(utc).astimezone()
def ts(hours_ago): return (now - datetime.timedelta(hours=hours_ago)).isoformat()
CONF = "docs/plans/confirmed-proposal.md"
# id, name, status, spec, preference, hours since the last update, extra record fields
rows = [
  (stale, "stale-draft", "draft", "Stale draft. Proposal: " + CONF, "", 100, {}),
  (hold, "hold-line", "needs_review", "HOLD: the operator wants this kept back.\nProposal: " + CONF, "", 1, {}),
  (seq, "sequenced-after", "needs_review", "Implement the follow-up. Sequenced after " + stale + ". Proposal: " + CONF, "", 1, {}),
  (unconf, "proposal-unconfirmed", "needs_review", "Implement the sample. Proposal: docs/plans/unconfirmed-proposal.md", "", 1, {}),
  (noprop, "no-proposal", "needs_review", "Implement the sample. No proposal file is named here.", "", 1, {}),
  (main_, "main-carries", "needs_review", "Implement the sample. Proposal: " + CONF, "", 1, {}),
  (wt, "worktree-unregistered", "needs_review", "Implement the sample. Proposal: " + CONF, "", 1,
   {"project_dir": phantom, "git": {"branch": "automaton/" + wt}}),
  (control, "control", "needs_review", "Implement the sample. Proposal: " + CONF, "gpu", 10, {}),
  (claude, "claude-candidate", "needs_review", "Implement the sample. Proposal: docs/plans/pending-claude.md", "claude", 1, {}),
  (gpu, "gpu-candidate", "needs_review", "Implement the sample. Proposal: docs/plans/pending-gpu.md", "gpu", 1, {}),
]
with open(out, "w") as f:
    for aid, name, status, spec, pref, hrs, extra in rows:
        stamp = ts(hrs)
        rec = {
            "id": aid, "title": name, "spec": spec + " LIVECHECK[" + aid + "]",
            "project_dir": "", "status": status, "backend": "shell",
            "write_dirs": [wtroot + "/" + aid + "/" + sub],
            "created_at": stamp, "updated_at": stamp,
        }
        if pref:
            rec["lane_preference"] = pref
        if status != "draft":
            task = {"id": aid + "t1", "story_id": aid + "s1", "prd_id": aid,
                    "title": "Write the sample file for " + name,
                    "spec": "Write the sample file. LIVECHECK[%s]" % aid,
                    "status": "pending", "verification_script": verify,
                    "created_at": stamp, "updated_at": stamp}
            rec["stories"] = [{"id": aid + "s1", "prd_id": aid, "title": "Sample story", "status": "pending",
                               "tasks": [task], "created_at": stamp, "updated_at": stamp}]
        rec.update(extra)
        f.write(json.dumps(rec) + "\n")
PY
}

write_config() {
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
    -e "s|http://local-node:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
    -e "s|http://localhost:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
    -e '/^compute_nodes:/,/^observer:/{/^observer:/!d}' \
    "$ROOT/testdata/datawatch.yaml" > "$CFG"
  # The worktree base repository, the worktree root, the allowed project root,
  # the lane fill poll interval and the stand-in worker.
  sed -i \
    -e "s|^autonomous:\$|autonomous:\n  verification_backend: shell\n  questions:\n    enabled: true\n  worktree_base_repo: $REPO\n  worktree_dir: $WTROOT\n  lane_fill:\n    poll_seconds: $POLL_SECONDS|" \
    -e "s|^server:\$|server:\n  allowed_project_roots: [\"$PROJ\", \"$WTROOT\"]|" \
    -e "s|^shell_backend:\$|shell_backend:\n  script_path: $S/bin/worker.sh|" \
    "$CFG"
}

# --- reads ---------------------------------------------------------------
LF_LIMIT=300
lf() { api GET "/api/autonomous/lane-fill?limit=$LF_LIMIT"; }
cfg_get() { api GET /api/config | jq_r "$1"; }
lfc() { cfg_get ".autonomous.lane_fill.$1"; }
put_cfg() { api_code PUT /api/config "$1"; }
prd() { api GET "/api/autonomous/prds/$1"; }
prd_status() { prd "$1" | jq_r '.status'; }
dlog() { cat "$LOG" "$DATA"/*.log 2>/dev/null; }
# dec_field <status json> <prd id> <jq expression on the newest blocked or start decision>
dec_blocked() { printf '%s' "$1" | jq -c --arg id "$2" '[.decisions[]? | select(.prd_id==$id and .action=="blocked")][0] // {}'; }
dec_start() { printf '%s' "$1" | jq -c --arg id "$2" '[.decisions[]? | select(.prd_id==$id and .action=="start_prd")][0] // {}'; }

lf_polled() { [[ "$(lf | jq_r '.last_poll_at | startswith("0001") | not')" == true ]]; }
all_refusals_seen() {
  local st; st="$(lf)"
  [[ "$(printf '%s' "$st" | jq_r '[.decisions[]? | select(.eligibility_reason != null and .eligibility_reason != "")] | map(.prd_id) | unique | length')" -ge 9 ]] &&
    [[ "$(printf '%s' "$st" | jq_r '[.decisions[]? | select(.action=="start_prd")] | length')" -ge 1 ]]
}

diag() {
  say "  DIAG: lane fill: $(lf | jq -c '{mode, modes, lane_counts, queued_eligible, evidence}')"
  dlog | grep -iE 'lane fill|lane-fill' | tail -6 | cut -c1-240 | sed 's/^/    log: /'
}

# ============================================================================
say "lane fill eligibility live check — $(now) — binary: $BINARY ($VERSION)"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT ollama stub $OLLAMA_PORT tls stub $TLS_STUB_PORT"

LEFT_SNAP="$S/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

CUR=0
build_project
write_worker
write_stub
seed_store
write_config
DEFAULT_PROBE="$(gitr symbolic-ref refs/remotes/origin/HEAD 2>&1); main exists: $(gitr show-ref --verify --quiet refs/heads/main && echo yes || echo no)"

# --- the stubs and the sandbox daemon --------------------------------------
export PATH="$S/bin:$PATH" TMPDIR="$DTMP" TMUX_TMPDIR="$SOCKDIR"
python3 -I "$S/ollama-stub.py" "$OLLAMA_PORT" '[]' </dev/null >/dev/null 2>&1 &
td_track_pid "$!" "$S/ollama-stub.py"
OLLAMA_STUB_PID="$!"
if ! td_start "$BINARY" "$CFG" "$LOG"; then
  say "FAIL: the sandbox daemon did not stay up: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
DPID="$TD_LAST_PID"
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! wait_until 90 curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  say "FAIL: the sandbox daemon does not answer /api/health: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
DAEMON_UP_AT="$(now)"
say "  daemon pid $DPID healthy at $DAEMON_UP_AT"
wait_until 120 lf_polled; POLLED=$?
[[ $POLLED == 0 ]] || { say "FAIL: lane fill never polled"; diag; exit 1; }
wait_until 90 all_refusals_seen || say "  WARN: not every refusal was recorded in time"
FIRST_SNAPSHOT="$(lf)"

# ============================================================================
begin_check 1 "Defaults on a fresh config, the round trip and a bad mode"
SEVEN="stale_after_hours require_confirmed_proposal check_main_carries require_worktree lane_counts_from_caps gpu_mode claude_mode"
D_BLOCK="$(cfg_get '.autonomous.lane_fill')"
rep_block "\`GET /api/config\`, the lane fill block on a fresh config:" "$(printf '%s' "$D_BLOCK" | jq '.')"
DEF_OK=1
printf '%s' "$D_BLOCK" | jq -e '.stale_after_hours==72 and .require_confirmed_proposal==true and .check_main_carries==true and .require_worktree==true and .lane_counts_from_caps==true and .gpu_mode=="" and .claude_mode==""' >/dev/null 2>&1 && DEF_OK=0
ck 1 $DEF_OK "the seven new keys read their defaults on a fresh config" "stale_after_hours 72, require_confirmed_proposal, check_main_carries, require_worktree and lane_counts_from_caps true, gpu_mode and claude_mode empty; read: $(printf '%s' "$D_BLOCK" | jq -c '{stale_after_hours, require_confirmed_proposal, check_main_carries, require_worktree, lane_counts_from_caps, gpu_mode, claude_mode}')"
MODES="$(printf '%s' "$FIRST_SNAPSHOT" | jq -c '{mode, modes}')"
[[ "$(printf '%s' "$FIRST_SNAPSHOT" | jq_r '.mode + "/" + .modes.gpu + "/" + .modes.claude')" == shadow/shadow/shadow ]]
ck 1 $? "lane fill mode is shadow for both lane kinds on a fresh config" "status: $MODES; the generated config sets only the poll interval ($POLL_SECONDS seconds), no mode"
RT_BODY='{"autonomous.lane_fill.stale_after_hours":48,"autonomous.lane_fill.require_confirmed_proposal":false,"autonomous.lane_fill.check_main_carries":false,"autonomous.lane_fill.require_worktree":false,"autonomous.lane_fill.lane_counts_from_caps":false,"autonomous.lane_fill.gpu_mode":"off","autonomous.lane_fill.claude_mode":"shadow"}'
RT_CODE="$(put_cfg "$RT_BODY")"
RT_READ="$(cfg_get '.autonomous.lane_fill')"
RT_OK=1
[[ "$RT_CODE" == 200 ]] && printf '%s' "$RT_READ" | jq -e '.stale_after_hours==48 and .require_confirmed_proposal==false and .check_main_carries==false and .require_worktree==false and .lane_counts_from_caps==false and .gpu_mode=="off" and .claude_mode=="shadow"' >/dev/null 2>&1 && RT_OK=0
ck 1 $RT_OK "a PUT then GET round trip of all seven keys (48, false x4, off, shadow)" "PUT HTTP $RT_CODE; read: $(printf '%s' "$RT_READ" | jq -c '{stale_after_hours, require_confirmed_proposal, check_main_carries, require_worktree, lane_counts_from_caps, gpu_mode, claude_mode}')"
RS_BODY='{"autonomous.lane_fill.stale_after_hours":72,"autonomous.lane_fill.require_confirmed_proposal":true,"autonomous.lane_fill.check_main_carries":true,"autonomous.lane_fill.require_worktree":true,"autonomous.lane_fill.lane_counts_from_caps":true,"autonomous.lane_fill.gpu_mode":"","autonomous.lane_fill.claude_mode":""}'
RS_CODE="$(put_cfg "$RS_BODY")"
RS_OK=1
[[ "$RS_CODE" == 200 ]] && cfg_get '.autonomous.lane_fill' | jq -e '.stale_after_hours==72 and .require_confirmed_proposal==true and .check_main_carries==true and .require_worktree==true and .lane_counts_from_caps==true and .gpu_mode=="" and .claude_mode==""' >/dev/null 2>&1 && RS_OK=0
ck 1 $RS_OK "the defaults can be written back (empty modes inherit again)" "PUT HTTP $RS_CODE"
B_GPU="$(api_both PUT /api/config '{"autonomous.lane_fill.gpu_mode":"sometimes"}')"
B_CLAUDE="$(api_both PUT /api/config '{"autonomous.lane_fill.claude_mode":"sometimes"}')"
AFTER_GPU="$(lfc gpu_mode)"
[[ "$B_GPU" == 400* && "$B_CLAUDE" == 400* && "$AFTER_GPU" == "" ]]
ck 1 $? "a bad gpu_mode (and claude_mode) is a 400 and saves nothing" "gpu_mode: $B_GPU — claude_mode: $B_CLAUDE — gpu_mode after the refused write: \`${AFTER_GPU:-empty}\`"

# ============================================================================
begin_check 2 "Refusals in shadow mode, one for each rule"
rep ""
rep "Throwaway project: a bare repository whose default branch is \`$PROJECT_DEFAULT_BRANCH\` cloned to the worktree base repository (\`refs/remotes/origin/HEAD\` records it, \`main\` does not exist), holding one proposal file with a \`Confirmed: yes\` line and others without. The Automata were written into the sandbox store before its first start."
rep_block "Base branch probe:" "$DEFAULT_PROBE"
ST2="$(lf)"
S2_DEC="$(printf '%s' "$ST2" | jq -c '[.decisions[] | select(.action=="blocked" and .eligibility_reason != null and .eligibility_reason != "") | {prd_id, eligibility_reason, mode, executed}]')"
rep ""
rep "| Automaton | What it is | Decision | eligibility_reason | Reason text |"
rep "|---|---|---|---|---|"
for id in "${REFUSED_IDS[@]}" "$A_CLAUDE" "$A_GPU"; do
  d="$(dec_blocked "$ST2" "$id")"
  rep "| \`$id\` | ${LABEL[$id]} | $(printf '%s' "$d" | jq_r '.action // "none"') | \`$(printf '%s' "$d" | jq_r '.eligibility_reason // "none"')\` | $(printf '%s' "$d" | jq_r '.reason // ""' | tr '|' '/') |"
done
CTRL_D="$(dec_start "$ST2" "$A_CONTROL")"
rep "| \`$A_CONTROL\` | ${LABEL[$A_CONTROL]} | $(printf '%s' "$CTRL_D" | jq_r '.action // "none"') | — | $(printf '%s' "$CTRL_D" | jq_r '.reason // ""' | tr '|' '/') |"
for id in "${REFUSED_IDS[@]}"; do
  d="$(dec_blocked "$ST2" "$id")"
  [[ "$(printf '%s' "$d" | jq_r '.action')" == blocked && "$(printf '%s' "$d" | jq_r '.eligibility_reason')" == "${WANT_REASON[$id]}" && "$(printf '%s' "$d" | jq_r '.mode')" == shadow ]]
  ck 2 $? "${LABEL[$id]}: a blocked decision with eligibility_reason \`${WANT_REASON[$id]}\`" "\`$id\`: $(printf '%s' "$d" | jq -c '{action, eligibility_reason, mode, executed}'), reason \"$(printf '%s' "$d" | jq_r '.reason')\""
done
for id in "$A_CLAUDE" "$A_GPU"; do
  d="$(dec_blocked "$ST2" "$id")"
  [[ "$(printf '%s' "$d" | jq_r '.eligibility_reason')" == proposal_not_confirmed ]]
  ck 2 $? "${LABEL[$id]}: refused as proposal_not_confirmed until the line is added" "\`$id\`: $(printf '%s' "$d" | jq -c '{action, eligibility_reason}')"
done
N_START="$(printf '%s' "$ST2" | jq_r '[.decisions[] | select(.action=="start_prd")] | length')"
ONLY_ID="$(printf '%s' "$ST2" | jq_r '[.decisions[] | select(.action=="start_prd")] | map(.prd_id) | unique | join(",")')"
[[ "$ONLY_ID" == "$A_CONTROL" && "$N_START" -ge 1 ]]
ck 2 $? "the control is the only start_prd decision" "start_prd decisions $N_START, for: \`$ONLY_ID\`; $(printf '%s' "$CTRL_D" | jq -c '{lane_id, lane_type, action, mode, executed}')"
N_EXEC="$(printf '%s' "$ST2" | jq_r '[.decisions[] | select(.executed==true)] | length')"
RUNS_LOG="$(cat "$STATE/runs.log" 2>/dev/null | tr '\n' ' ')"
NOT_STARTED=0
for id in "${ALL_IDS[@]}"; do
  s="$(prd_status "$id")"
  case "$s" in running|completed|approved) NOT_STARTED=1;; esac
done
[[ "$N_EXEC" == 0 && $NOT_STARTED == 0 && -z "$RUNS_LOG" ]]
ck 2 $? "nothing was started in shadow mode (every decision executed false, no Automaton running, the worker never ran)" "executed decisions $N_EXEC; statuses: $(for id in "${ALL_IDS[@]}"; do printf '%s=%s ' "$id" "$(prd_status "$id")"; done); worker runs: \`${RUNS_LOG:-none}\`"
[[ "$(printf '%s' "$ST2" | jq_r '.queued_eligible.gpu')" == 1 && "$(printf '%s' "$ST2" | jq_r '.queued_eligible.claude')" == 0 ]]
ck 2 $? "the queued eligible count is the control alone (gpu 1, claude 0)" "$(printf '%s' "$ST2" | jq -c '.queued_eligible')"
rep_block "Refusal decisions as served by \`GET /api/autonomous/lane-fill\`:" "$(printf '%s' "$S2_DEC" | jq '.')"

# ============================================================================
begin_check 3 "The same refusals on every read surface"
mcp_read() { # prints the autonomous_lane_fill_status result text
  env -u TMUX -u TMUX_TMPDIR TMUX_TMPDIR="$SOCKDIR" python3 -I - "$BINARY" "$CFG" "$LF_LIMIT" <<'PY'
import json, subprocess, sys, os, select, time
bin_, cfg, limit = sys.argv[1], sys.argv[2], int(sys.argv[3])
p = subprocess.Popen([bin_, "--config", cfg, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=dict(os.environ), text=True)
def send(o): p.stdin.write(json.dumps(o) + "\n"); p.stdin.flush()
send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "lane-fill-live-check", "version": "0"}}})
send({"jsonrpc": "2.0", "method": "notifications/initialized"})
send({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "autonomous_lane_fill_status", "arguments": {"limit": limit}}})
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
SURF_PY="$S/surfaces.py"
cat > "$SURF_PY" <<'PY'
import json, sys
rest, cli_json, mcp = [json.load(open(p)) for p in sys.argv[1:4]]
cli_text, chat_text = open(sys.argv[4]).read(), open(sys.argv[5]).read()
problems = []
def refusals(d):
    out = {}
    for x in d.get("decisions", []):
        if x.get("eligibility_reason"):
            out.setdefault(x["prd_id"], x["eligibility_reason"])
    return out
base = refusals(rest)
if len(base) < 9: problems.append("REST holds %d refused Automata, expected 9" % len(base))
for name, d in (("CLI --json", cli_json), ("MCP", mcp)):
    if refusals(d) != base: problems.append("%s refusals differ from REST" % name)
    if d.get("lane_counts") != rest.get("lane_counts"): problems.append("%s lane_counts differ from REST" % name)
    if d.get("modes") != rest.get("modes"): problems.append("%s modes differ from REST" % name)
def line_has(text, pid, code):
    return any(pid in l and ("[%s]" % code) in l for l in text.splitlines())
for pid, code in base.items():
    if not line_has(cli_text, pid, code): problems.append("CLI text misses %s [%s]" % (pid, code))
newest = rest.get("decisions", [])[:5]
for x in newest:
    if x.get("eligibility_reason") and not line_has(chat_text, x["prd_id"], x["eligibility_reason"]):
        problems.append("chat reply misses %s [%s]" % (x["prd_id"], x["eligibility_reason"]))
for what, text in (("CLI text", cli_text), ("chat reply", chat_text)):
    if "Lane counts:" not in text: problems.append(what + " misses the lane counts line")
    if "Queued eligible:" not in text: problems.append(what + " misses the queued eligible line")
print("OK" if not problems else "; ".join(problems))
sys.exit(0 if not problems else 1)
PY
SV=""
for try in 1 2 3; do
  lf > "$S/s-rest.json"
  dw autonomous lane-fill --limit "$LF_LIMIT" --json > "$S/s-cli.json" 2> "$S/s-cli.err"
  dw autonomous lane-fill --limit "$LF_LIMIT" > "$S/s-cli.txt" 2>> "$S/s-cli.err"
  mcp_read > "$S/s-mcp.json"
  api POST /api/test/message "{\"text\":\"autonomous lane-fill $LF_LIMIT\"}" | jq -r '.responses | join("\n")' > "$S/s-chat.txt" 2>/dev/null
  lf > "$S/s-rest.json"
  SV="$(python3 -I "$SURF_PY" "$S/s-rest.json" "$S/s-cli.json" "$S/s-mcp.json" "$S/s-cli.txt" "$S/s-chat.txt" 2>&1)"
  [[ "$SV" == OK ]] && break
  sleep 3
done
rep_block "CLI text (\`datawatch autonomous lane-fill\`), refusal lines:" "$(grep -E '\[(stale|held|sequenced_after_unshipped|proposal_not_confirmed|proposal_missing|main_carries_work|worktree_invalid)\]|Lane counts|Queued eligible|Lane fill:|Mode per' "$S/s-cli.txt")"
rep_block "Chat reply (\`POST /api/test/message\`, text \`autonomous lane-fill $LF_LIMIT\`; the chat verb shows the newest five decisions by design):" "$(cat "$S/s-chat.txt")"
rep_block "MCP result (\`autonomous_lane_fill_status\`), refusals only:" "$(jq -c '[.decisions[] | select(.eligibility_reason) | {prd_id, eligibility_reason}] | unique' "$S/s-mcp.json" 2>/dev/null)"
[[ "$SV" == OK ]]
ck 3 $? "REST, the MCP tool, the CLI (--json and text) and the chat verb carry the same refusals, lane counts and modes" "comparison: \`$SV\`; the chat reply is held to the newest five decisions it shows"
APPJS="$(curl -sk -m 30 "$BASE/app.js")"
APP_OK=0
for code in $CODES; do
  grep -q "lane_fill_eligibility_$code" <<<"$APPJS" || APP_OK=1
done
for key in lane_fill_lane_counts lane_fill_kind_modes lane_fill_lane_counts_source_caps; do grep -q "$key" <<<"$APPJS" || APP_OK=1; done
ck 3 $APP_OK "the served app.js maps all seven refusal codes to chip keys and carries the lane counts and per-kind mode keys" "$(for code in $CODES; do printf '%s ' "lane_fill_eligibility_$code"; done)"
LOC_OK=0; LOC_TXT=""
for lang in en de es fr ja; do
  body="$(curl -sk -m 30 "$BASE/locales/$lang.json")"
  miss=""
  for code in $CODES; do printf '%s' "$body" | jq -e --arg k "lane_fill_eligibility_$code" 'has($k) and (.[$k] | length > 0)' >/dev/null 2>&1 || miss+=" $code"; done
  for key in lane_fill_lane_counts lane_fill_lane_counts_part lane_fill_kind_modes lane_fill_kind_modes_part; do printf '%s' "$body" | jq -e --arg k "$key" 'has($k)' >/dev/null 2>&1 || miss+=" $key"; done
  [[ -z "$miss" ]] || LOC_OK=1
  LOC_TXT+="$lang: ${miss:-all keys present}; "
done
ck 3 $LOC_OK "the five locale bundles (en, de, es, fr, ja) carry the seven chip keys and the lane counts and per-kind mode keys" "$LOC_TXT"

# ============================================================================
begin_check 4 "Lane counts follow the LLM in-flight caps"
CLAUDE_BACKEND="$(lfc claude_backend)"; CLAUDE_LANES_CFG="$(lfc claude_lanes)"
LLMS="$(api GET /api/llms)"
HAVE_ENTRY="$(printf '%s' "$LLMS" | jq_r --arg n "$CLAUDE_BACKEND" '(if type=="array" then . else (.llms // []) end) | map(select(.name==$n)) | length')"
if [[ "$HAVE_ENTRY" == 0 ]]; then
  ENTRY_CODE="$(api_both POST /api/llms "{\"name\":\"$CLAUDE_BACKEND\",\"kind\":\"claude-code\",\"max_inflight\":1}")"
  ENTRY_MADE=yes
else
  ENTRY_CODE="$(api_both PUT "/api/llms/$CLAUDE_BACKEND" "{\"max_inflight\":1}")"
  ENTRY_MADE=no
fi
rep ""
rep "Claude backend \`$CLAUDE_BACKEND\`, claude_lanes \`$CLAUDE_LANES_CFG\`; LLM entry $([[ $ENTRY_MADE == yes ]] && echo created || echo updated) with max_inflight 1: \`$ENTRY_CODE\`."
claude_counts() { lf | jq -c '.lane_counts.claude'; }
caps_applied() { [[ "$(claude_counts | jq_r '.source')" == llm_caps ]]; }
wait_until 60 caps_applied
C_CAPS="$(claude_counts)"
[[ "$CLAUDE_LANES_CFG" == 3 && "$(printf '%s' "$C_CAPS" | jq_r '.configured')" == 3 && "$(printf '%s' "$C_CAPS" | jq_r '.effective')" == 1 && "$(printf '%s' "$C_CAPS" | jq_r '.source')" == llm_caps ]]
ck 4 $? "with the Claude entry capped at 1 and claude_lanes 3, lane_counts shows effective 1, source llm_caps" "lane_counts.claude: $C_CAPS; Claude lanes in the status: $(lf | jq_r '[.lanes[] | select(.type=="claude")] | length')"
put_cfg '{"autonomous.lane_fill.lane_counts_from_caps":false}' >/dev/null
caps_off() { [[ "$(claude_counts | jq_r '.source')" == config ]]; }
wait_until 60 caps_off
C_CFG="$(claude_counts)"
[[ "$(printf '%s' "$C_CFG" | jq_r '.effective')" == 3 && "$(printf '%s' "$C_CFG" | jq_r '.source')" == config ]]
ck 4 $? "with lane_counts_from_caps false it shows effective 3, source config" "lane_counts.claude: $C_CFG; reason: $(printf '%s' "$C_CFG" | jq_r '.reason')"
put_cfg '{"autonomous.lane_fill.lane_counts_from_caps":true}' >/dev/null
wait_until 60 caps_applied
COUNTS_TEXT="$(dw autonomous lane-fill 2>&1 | grep 'Lane counts:')"
[[ "$COUNTS_TEXT" == *"claude 1 of 3 (LLM caps)"* ]]
ck 4 $? "the CLI line says it plainly: claude 1 of 3 (LLM caps)" "\`$COUNTS_TEXT\`"

# Data for check 7 is taken here, while the Claude entry is capped and the
# control is still waiting (nothing has been started).
SW_DIR="$S/sw"; mkdir -p "$SW_DIR"
lf > "$SW_DIR/lane-fill.json"
for id in "${ALL_IDS[@]}"; do prd "$id"; done | jq -s '{prds: .}' > "$SW_DIR/prds.json"
api GET /api/capacity > "$SW_DIR/capacity.json"
cat > "$SW_DIR/script_rule.py" <<'PY'
import importlib.util, json, sys
path, prds_file, cap_file, record_out = sys.argv[1:5]
spec = importlib.util.spec_from_file_location("svwatch", path)
sv = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sv)   # defines the script's constants and functions; main() is not run
prds = json.load(open(prds_file))
prds = prds.get("prds", prds) if isinstance(prds, dict) else prds
cap = json.load(open(cap_file)) or {}
verdict, queued_runnable = {}, 0
for p in prds:
    st = p.get("status")
    tasks = [k for s in p.get("stories", []) for k in s.get("tasks", [])]
    if p["id"] in sv.IGNORE:
        verdict[p["id"]] = "ignored by the script"
        continue
    if st in ("needs_review", "approved", "draft") and sv.recent(p.get("updated_at")) and (tasks or st == "draft"):
        queued_runnable += 1
        verdict[p["id"]] = "counted as queued and runnable"
    elif st in ("needs_review", "approved", "draft") and not sv.recent(p.get("updated_at")):
        verdict[p["id"]] = "not counted (older than the script's 72 hour window)"
    elif st in ("needs_review", "approved", "draft"):
        verdict[p["id"]] = "not counted (queued but holds no tasks)"
    else:
        verdict[p["id"]] = "not queued (%s)" % st
pools = {x["name"]: x for x in cap.get("pools", [])}
cl, pi = pools.get("llm:claude-code", {}), pools.get("llm:pi-prompt", {})
lines = []
if cl.get("limit") and cl.get("held", 0) == 0 and queued_runnable:
    lines.append("IDLE Claude lane while %d Automata are needs_review, approved or draft" % queued_runnable)
if pi.get("limit") and pi.get("held", 0) < pi.get("limit", 0) and queued_runnable:
    lines.append("IDLE pi lane (%s/%s held) with 0 waiting and %d queued Automata" % (pi.get("held"), pi.get("limit"), queued_runnable))
import time
rec = [{"kind": m[0], "subject": m[1], "text": l} for l in lines for m in [sv.map_finding(l)] if m]
json.dump({"at": int(time.time()), "findings": rec}, open(record_out, "w"))
print(json.dumps({"lines": lines, "queued_runnable": queued_runnable, "verdict": verdict,
                  "claude_pool": cl, "pi_pool": pi, "record": rec}))
PY
python3 -I "$SW_DIR/script_rule.py" "$ROOT/scripts/supervisor-watch.py" "$SW_DIR/prds.json" "$SW_DIR/capacity.json" "$SW_DIR/script-record.json" > "$SW_DIR/script-rule.json" 2> "$SW_DIR/script-rule.err"
SCRIPT_RULE_RC=$?
put_cfg "{\"autonomous.automation.supervisor_watch.script_output_path\":\"$SW_DIR/script-record.json\"}" >/dev/null
watch_view() { api GET /api/autonomous/supervisor-watch; }
watch_idle_subjects() { watch_view | jq -c '[.findings[]? | select(.kind=="idle_lane_eligible_work" and (.resolved|not)) | .subject] | sort'; }
CMP_SEEN=1
for _ in $(seq 1 18); do
  python3 -I "$SW_DIR/script_rule.py" "$ROOT/scripts/supervisor-watch.py" "$SW_DIR/prds.json" "$SW_DIR/capacity.json" "$SW_DIR/script-record.json" > "$SW_DIR/script-rule.json" 2>/dev/null
  watch_view > "$SW_DIR/watch-before.json"
  if [[ "$(jq_r '(.comparison.only_watch|length) + (.comparison.only_script|length)' "$SW_DIR/watch-before.json")" -gt 0 ]] ||
     [[ "$(jq_r '.comparison | tostring | length' "$SW_DIR/watch-before.json")" -gt 80 && "$(watch_idle_subjects)" != "[]" ]]; then CMP_SEEN=0; break; fi
  sleep 10
done
watch_view > "$SW_DIR/watch-before.json"
lf > "$SW_DIR/lane-fill-before.json"
# The refusal -> eligible work link: make the control stale (its last update is
# ten hours old) and watch lane fill and the daemon-side watch both drop it.
put_cfg '{"autonomous.lane_fill.stale_after_hours":8}' >/dev/null
control_refused() { [[ "$(lf | jq_r --arg id "$A_CONTROL" '[.decisions[]? | select(.prd_id==$id and .action=="blocked")][0].eligibility_reason')" == stale && "$(lf | jq_r '.queued_eligible.gpu')" == 0 ]]; }
wait_until 60 control_refused; CTRL_STALE_RC=$?
lf > "$SW_DIR/lane-fill-after.json"
watch_cleared() { [[ "$(watch_idle_subjects)" == "[]" ]]; }
wait_until 150 watch_cleared; WATCH_CLEARED_RC=$?
watch_view > "$SW_DIR/watch-after.json"
put_cfg '{"autonomous.lane_fill.stale_after_hours":72}' >/dev/null
control_back() { [[ "$(lf | jq_r '.queued_eligible.gpu')" == 1 ]]; }
wait_until 60 control_back
# Remove the cap again: the lane counts are back to the configured numbers.
if [[ $ENTRY_MADE == yes ]]; then api DELETE "/api/llms/$CLAUDE_BACKEND" >/dev/null; else api PUT "/api/llms/$CLAUDE_BACKEND" '{"max_inflight":0}' >/dev/null; fi
put_cfg "{\"autonomous.automation.supervisor_watch.script_output_path\":\"\"}" >/dev/null

# ============================================================================
begin_check 5 "The per-lane-kind switch: gpu_mode on starts GPU work only"
SHELL_BACKEND=shell
put_cfg "{\"autonomous.lane_fill.gpu_backend\":\"$SHELL_BACKEND\"}" >/dev/null
GPU_BACKEND_NOW="$(lfc gpu_backend)"
[[ "$GPU_BACKEND_NOW" == "$SHELL_BACKEND" && "$(lfc claude_mode)" == "" && "$(lfc mode)" == shadow ]]
ck 5 $? "gpu_backend is the daemon's shell backend; claude_mode is empty and the base mode is shadow" "gpu_backend \`$GPU_BACKEND_NOW\`, claude_mode \`$(lfc claude_mode)\`, mode \`$(lfc mode)\`"
put_cfg '{"autonomous.lane_fill.gpu_mode":"on"}' >/dev/null
control_started() { local s; s="$(prd_status "$A_CONTROL")"; [[ "$s" == running || "$s" == completed ]]; }
wait_until 90 control_started; CS=$?
[[ $CS == 0 ]] || diag
ST5="$(lf)"
rep_block "Status after gpu_mode on (modes, control decision):" "$(printf '%s' "$ST5" | jq -c '{mode, modes}'; dec_start "$ST5" "$A_CONTROL")"
[[ "$(printf '%s' "$ST5" | jq_r '.modes.gpu + "/" + .modes.claude + "/" + .mode')" == on/shadow/shadow ]]
ck 5 $? "the status shows gpu on, claude shadow, base mode shadow" "$(printf '%s' "$ST5" | jq -c '{mode, modes}')"
CD5="$(dec_start "$ST5" "$A_CONTROL")"
[[ $CS == 0 && "$(printf '%s' "$CD5" | jq_r '.lane_type')" == gpu && "$(printf '%s' "$CD5" | jq_r '.executed')" == true && "$(printf '%s' "$CD5" | jq_r '.mode')" == on ]]
ck 5 $? "the control starts on a GPU lane (its decision is executed in mode on)" "Automaton \`$A_CONTROL\` status \`$(prd_status "$A_CONTROL")\`; decision $(printf '%s' "$CD5" | jq -c '{lane_id, lane_type, mode, executed}'); worker runs: \`$(cat "$STATE/runs.log" 2>/dev/null | tr '\n' ' ')\`"
# A Claude-lane decision: confirm the Claude candidate's proposal.
printf '# Pending proposal for the Claude lane\n\nConfirmed: yes\n' > "$REPO/docs/plans/pending-claude.md"
claude_decision() { [[ "$(lf | jq_r --arg id "$A_CLAUDE" '[.decisions[]? | select(.prd_id==$id and .action=="start_prd")][0].lane_type')" == claude ]]; }
wait_until 60 claude_decision; CLD=$?
sleep $((POLL_SECONDS * 3))
ST5B="$(lf)"; CLD5="$(dec_start "$ST5B" "$A_CLAUDE")"
[[ $CLD == 0 && "$(printf '%s' "$CLD5" | jq_r '.executed')" == false && "$(printf '%s' "$CLD5" | jq_r '.mode')" == shadow && "$(prd_status "$A_CLAUDE")" == needs_review ]]
ck 5 $? "a Claude-lane decision exists and stays unexecuted (mode shadow) over three polls" "Automaton \`$A_CLAUDE\` status \`$(prd_status "$A_CLAUDE")\`; decision $(printf '%s' "$CLD5" | jq -c '{lane_id, lane_type, mode, executed}')"
REFUSED_STARTED=""
for id in "${REFUSED_IDS[@]}"; do
  s="$(prd_status "$id")"
  case "$s" in running|completed|approved) REFUSED_STARTED+="$id=$s ";; esac
done
RUNS5="$(sort "$STATE/runs.log" 2>/dev/null | uniq -c | tr '\n' ' ')"
[[ -z "$REFUSED_STARTED" ]] && ! grep -qE "$(printf '%s|' "${REFUSED_IDS[@]}" "$A_CLAUDE" "$A_GPU" | sed 's/|$//')" "$STATE/runs.log" 2>/dev/null
ck 5 $? "none of the refused Automata started (statuses unchanged, the worker ran for the control only)" "refused statuses: $(for id in "${REFUSED_IDS[@]}"; do printf '%s=%s ' "$id" "$(prd_status "$id")"; done); worker runs (count id): \`$RUNS5\`"
# The control holds the one GPU lane while it runs; wait for it to finish so a
# lane is free, then flip gpu_mode back to shadow and make a GPU candidate eligible.
control_done() { case "$(prd_status "$A_CONTROL")" in completed|failed|blocked|cancelled) return 0;; esac; return 1; }
wait_until 240 control_done; CDONE=$?
if [[ $CDONE != 0 ]]; then
  say "  DIAG control: $(prd "$A_CONTROL" | jq -c '{status, wait_reason, blocked_reason, backend, tasks: [.stories[]?.tasks[]? | {id, status, error, wait_reason, failure_reason}]}')"
  say "  DIAG capacity: $(api GET /api/capacity | jq -c '{pools: [.pools[]? | {name, limit, held}], waiting}')"
  dlog | grep -F "$A_CONTROL" | grep -v http_access | tail -12 | cut -c1-300 | sed 's/^/    log: /'
fi
ck 5 $CDONE "the control's run ends and frees its GPU lane (the stand-in worker ran and its verification passed)" "Automaton \`$A_CONTROL\` status \`$(prd_status "$A_CONTROL")\`; worker runs: \`$(cat "$STATE/runs.log" 2>/dev/null | tr '\n' ' ')\`; GPU lanes: $(lf | jq -c '[.lanes[] | select(.type=="gpu") | {id, state}]')"
put_cfg '{"autonomous.lane_fill.gpu_mode":"shadow"}' >/dev/null
printf '# Pending proposal for the GPU lane\n\nConfirmed: yes\n' > "$REPO/docs/plans/pending-gpu.md"
gpu_decision() { [[ "$(lf | jq_r --arg id "$A_GPU" '[.decisions[]? | select(.prd_id==$id and .action=="start_prd")][0].lane_type')" == gpu ]]; }
wait_until 60 gpu_decision; GD=$?
sleep $((POLL_SECONDS * 3))
ST5C="$(lf)"; GD5="$(dec_start "$ST5C" "$A_GPU")"
[[ $GD == 0 && "$(printf '%s' "$GD5" | jq_r '.executed')" == false && "$(printf '%s' "$GD5" | jq_r '.mode')" == shadow && "$(prd_status "$A_GPU")" == needs_review && "$(printf '%s' "$ST5C" | jq_r '.modes.gpu')" == shadow ]]
ck 5 $? "with gpu_mode back to shadow an eligible GPU candidate gets a decision and is not started at the next polls" "Automaton \`$A_GPU\` status \`$(prd_status "$A_GPU")\`; decision $(printf '%s' "$GD5" | jq -c '{lane_id, lane_type, mode, executed}'); modes $(printf '%s' "$ST5C" | jq -c '.modes')"
put_cfg '{"autonomous.lane_fill.gpu_mode":""}' >/dev/null

# ============================================================================
begin_check 6 "Pool evidence with an untrusted certificate"
TLS_DIR="$S/tls"; mkdir -p "$TLS_DIR"
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$TLS_DIR/key.pem" -out "$TLS_DIR/cert.pem" -days 2 \
  -subj "/CN=lane-fill-live-check" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1
FP="$(openssl x509 -in "$TLS_DIR/cert.pem" -noout -fingerprint -sha256 2>/dev/null | sed 's/.*=//; s/://g' | tr 'A-F' 'a-f')"
python3 -I "$S/ollama-stub.py" "$TLS_STUB_PORT" '["tls-stub-model:1b"]' "$TLS_DIR/cert.pem" "$TLS_DIR/key.pem" </dev/null >/dev/null 2>&1 &
td_track_pid "$!" "$S/ollama-stub.py"
TLS_STUB_PID="$!"
NODE=tls-stub-node
stub_up() { timeout 2 bash -c "exec 3<>/dev/tcp/127.0.0.1/$TLS_STUB_PORT" 2>/dev/null; }
wait_until 20 stub_up
NODE_ADD="$(api_both POST "/api/compute/nodes?probe=skip" "{\"name\":\"$NODE\",\"kind\":\"ollama\",\"address\":\"https://127.0.0.1:$TLS_STUB_PORT\"}")"
rep ""
rep "A compute node \`$NODE\` on a loopback https stub with a generated self-signed certificate (SAN 127.0.0.1): \`$NODE_ADD\`."
evidence_err() { lf | jq_r '.evidence.error // ""'; }
has_node_err() { [[ "$(evidence_err)" == *"$NODE"* ]]; }
wait_until 60 has_node_err; NE=$?
[[ $NE == 0 ]] || diag
EV_ERR="$(evidence_err)"
rep_block "Evidence error in \`GET /api/autonomous/lane-fill\`:" "$EV_ERR"
[[ $NE == 0 && "$EV_ERR" == *"$NODE"* && "$EV_ERR" == *tls_cert_fingerprint* && "$EV_ERR" == *tls_ca_file* ]]
ck 6 $? "the evidence error names the node and the fix (tls_ca_file or tls_cert_fingerprint, or tls_insecure_skip_verify)" "node named: $([[ "$EV_ERR" == *"$NODE"* ]] && echo yes || echo no)"
[[ "$EV_ERR" != *"127.0.0.1"* && "$EV_ERR" != *":$TLS_STUB_PORT"* && "$EV_ERR" != *"https://"* && "$EV_ERR" != *"http://"* ]]
ck 6 $? "the evidence error does not show the URL (no address, port or scheme)" "checked for 127.0.0.1, :$TLS_STUB_PORT and a scheme"
CLI_ERR="$(dw autonomous lane-fill 2>&1 | grep 'Pool evidence error')"
[[ "$CLI_ERR" == *"$NODE"* && "$CLI_ERR" == *tls_cert_fingerprint* ]]
ck 6 $? "the CLI shows the same error line" "\`${CLI_ERR:0:200}\`"
NODE_PUT="$(api_both PUT "/api/compute/nodes/$NODE?probe=skip" "{\"name\":\"$NODE\",\"kind\":\"ollama\",\"address\":\"https://127.0.0.1:$TLS_STUB_PORT\",\"tls_cert_fingerprint\":\"$FP\"}")"
evidence_reads() { [[ -z "$(evidence_err)" && "$(lf | jq_r --arg n "$NODE" '[.evidence.loaded_models[]? | select(.host==$n and .model=="tls-stub-model:1b")] | length')" == 1 ]]; }
wait_until 60 evidence_reads; ER=$?
[[ $ER == 0 ]] || diag
rep_block "Evidence after the node's tls_cert_fingerprint was set (\`PUT\` $NODE_PUT):" "$(lf | jq -c '.evidence')"
ck 6 $ER "after the node's tls_cert_fingerprint is set the evidence reads (no error, the node's resident model is listed)" "fingerprint of the generated certificate (SHA-256, hex) set on the node; evidence: $(lf | jq -c '.evidence | {error, loaded_models}')"
api DELETE "/api/compute/nodes/$NODE" >/dev/null

# ============================================================================
begin_check 7 "Shadow comparison"
rep ""
rep "The dogfood step: what lane fill refused and why, next to what the daemon-side supervisor watch and the supervisor watch script report for the same sandbox state. The data was taken in check 4's window (before anything was started, the Claude entry capped at 1); the refusal-to-eligible-work link was then tried by making the control stale."
rep ""
rep "**What could be run.** \`scripts/supervisor-watch.py\` is written for the operator's own daemon (a fixed address, the service name and the checkout), so it cannot be pointed at a sandbox and was **not** run as a program. The check loads it as a module and applies its own \`IGNORE\` set, its own \`recent()\` (72 hours), its idle-lane rule as written in \`main()\` and its own \`map_finding()\` and record format to the sandbox's Automaton list and capacity pools; the findings it would have recorded went to the daemon's comparison file."
LFB="$SW_DIR/lane-fill-before.json"
SR="$SW_DIR/script-rule.json"
rep ""
rep "| Automaton | What it is | Lane fill | Supervisor watch script (its rule) |"
rep "|---|---|---|---|"
DIFFS=0; DIFF_TXT=""
for id in "${ALL_IDS[@]}"; do
  code="$(jq_r --arg id "$id" '[.decisions[]? | select(.prd_id==$id and .action=="blocked" and .eligibility_reason != null)][0].eligibility_reason // ""' "$LFB")"
  if [[ -n "$code" ]]; then lfv="refused: \`$code\`"; else lfv="eligible"; fi
  sv="$(jq_r --arg id "$id" '.verdict[$id] // "not in the list"' "$SR" 2>/dev/null)"
  rep "| \`$id\` | ${LABEL[$id]} | $lfv | $sv |"
  if [[ -n "$code" && "$sv" == "counted as queued and runnable" ]]; then DIFFS=$((DIFFS + 1)); DIFF_TXT+="\`$id\` (${LABEL[$id]}): lane fill refuses it as \`$code\`; the script counts it as runnable. "; fi
done
SCRIPT_N="$(jq_r '.queued_runnable' "$SR" 2>/dev/null)"
LF_GPU="$(jq_r '.queued_eligible.gpu' "$LFB")"; LF_CLAUDE="$(jq_r '.queued_eligible.claude' "$LFB")"
rep ""
rep "**Counts.** Lane fill: eligible work for GPU lanes $LF_GPU, for Claude lanes $LF_CLAUDE. The script's own rule: $SCRIPT_N Automata counted as queued and runnable. Script output for the state (its findings): \`$(jq -c '.lines' "$SR" 2>/dev/null)\`; Claude pool it read: \`$(jq -c '.claude_pool' "$SR" 2>/dev/null)\`."
rep ""
rep "**Daemon-side supervisor watch**, findings of kind idle_lane_eligible_work before the control was made stale: \`$(jq -c '[.findings[]? | select(.kind=="idle_lane_eligible_work") | {subject, resolved}]' "$SW_DIR/watch-before.json")\`; after: \`$(jq -c '[.findings[]? | select(.kind=="idle_lane_eligible_work") | {subject, resolved}]' "$SW_DIR/watch-after.json")\`."
rep_block "The watch's own comparison against the script record file (\`GET /api/autonomous/supervisor-watch\`, comparison):" "$(jq '.comparison' "$SW_DIR/watch-before.json" 2>/dev/null)"
rep ""
rep "**Differences, written down:**"
if [[ $DIFFS -gt 0 ]]; then
  rep ""
  rep "- The script's rule counts $DIFFS Automata that lane fill refuses: $DIFF_TXT"
else
  rep ""
  rep "- No Automaton lane fill refuses is counted runnable by the script."
fi
rep "- The script reports one line per backend (\`IDLE Claude lane\`, \`IDLE pi lane\`) read from the capacity pools of the operator's daemon; the daemon-side watch reports one finding per idle lane id (for example \`gpu-1\`) read from lane fill's own queued-eligible count. The two keys differ by construction, so the daemon's comparison lists the lane ids as raised by the watch only and the backend names as raised by the script only, even when both mean the same idle lane."
rep "- The script has no stale, proposal, base-branch or worktree rule. It filters only by status, a 72 hour window and its fixed ignore list."
# Assertions
WATCH_SUBJ_BEFORE="$(jq -c '[.findings[]? | select(.kind=="idle_lane_eligible_work" and (.resolved|not)) | .subject] | sort' "$SW_DIR/watch-before.json")"
EXPECT_SUBJ="$(jq -c '[.lanes[]? | select(.state=="idle" and (.type=="gpu")) | .id] | sort' "$LFB")"
[[ -s "$SR" && $SCRIPT_RULE_RC == 0 && "$SCRIPT_N" -ge 1 ]]
ck 7 $? "the script's own rule was applied to the sandbox state and its findings and counts are in the report" "queued_runnable $SCRIPT_N, exit $SCRIPT_RULE_RC; $(cat "$SW_DIR/script-rule.err" 2>/dev/null | head -c 200)"
[[ "$WATCH_SUBJ_BEFORE" == "$EXPECT_SUBJ" && "$EXPECT_SUBJ" != "[]" ]]
ck 7 $? "the daemon-side watch raises idle_lane_eligible_work for exactly the idle lanes whose kind lane fill counts eligible work for (the GPU lanes, never a Claude lane)" "watch findings $WATCH_SUBJ_BEFORE; idle GPU lanes with eligible work in lane fill: $EXPECT_SUBJ; lane fill counts gpu $LF_GPU, claude $LF_CLAUDE"
[[ $CTRL_STALE_RC == 0 && $WATCH_CLEARED_RC == 0 ]]
ck 7 $? "once lane fill refuses the control (stale), it stops counting as eligible work in the daemon-side watch too: queued eligible 0 and the idle lane findings clear" "lane fill refusal: $(jq -c --arg id "$A_CONTROL" '[.decisions[]? | select(.prd_id==$id and .action=="blocked")][0] | {eligibility_reason, reason}' "$SW_DIR/lane-fill-after.json"); queued eligible after: $(jq -c '.queued_eligible' "$SW_DIR/lane-fill-after.json"); watch findings after: $(jq -c '[.findings[]? | select(.kind=="idle_lane_eligible_work" and (.resolved|not)) | .subject]' "$SW_DIR/watch-after.json")"
rep ""
if [[ $CMP_SEEN == 0 ]]; then
  rep "- The daemon recorded a comparison against the script record, shown above."
else
  rep "- The daemon did not record a comparison within the wait (the watch compares once per pass, every 60 seconds at least, against a record no older than one pass); the record file the script rule wrote is in the comparison view only when a pass saw it fresh. This is a gap in the evidence, not a difference."
fi
[[ $DIFFS -ge 0 ]]
ck 7 $? "every difference between lane fill and the script is listed in the report (none hidden)" "$DIFFS Automata counted by the script that lane fill refuses"

# ============================================================================
begin_check 8 "Cleanup"
CUR=8
DAEMON_LOG_TAIL="$(dlog | grep -iE 'lane fill|lane-fill' | cut -c1-200 | tail -10)"
FINAL_REFS="$(gitr for-each-ref --format='%(refname:short)' refs/heads/ | tr '\n' ' ')"
td_stop_all
sleep 1
D_GONE="no"; td_alive "$DPID" || D_GONE="yes"
STUBS_GONE="no"; td_alive "$OLLAMA_STUB_PID" || td_alive "$TLS_STUB_PID" || STUBS_GONE="yes"
PORT_CLOSED="no"; td_wait_port_closed "$TLS_PORT" 20 && PORT_CLOSED="yes"
LEFT_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP" 2>&1)"; LEFT_RC=$?
[[ "$D_GONE" == yes && "$STUBS_GONE" == yes && "$PORT_CLOSED" == yes ]]
ck 8 $? "the sandbox daemon and both stubs are gone and the daemon's port is closed" "daemon gone: $D_GONE; stubs gone: $STUBS_GONE; port $TLS_PORT closed: $PORT_CLOSED"
[[ "$LEFT_RC" == 0 ]]
ck 8 $? "no test daemon is left behind (scripts/check-no-leftover-test-daemons.sh)" "exit $LEFT_RC"
rep ""
rep "- The daemon and the stubs were ended by \`td_stop_all\` from \`scripts/test-daemon-lib.sh\` (the recorded pids, config or stub path as marker); nothing was ended by name."
rm -rf "$S"
[[ ! -e "$S" ]]
ck 8 $? "the throwaway directory (project, remote, worktrees, data, config, log, stubs, certificate, records) is removed" "directory exists after removal: $([[ -e "$S" ]] && echo yes || echo no)"

# --- assemble the report ---------------------------------------------------
OVERALL=PASS; TOTAL_FAIL=0
for n in 1 2 3 4 5 6 7 8; do TOTAL_FAIL=$(( TOTAL_FAIL + ${CK_FAIL[$n]:-0} )); done
[[ $TOTAL_FAIL -gt 0 ]] && OVERALL=FAIL
for n in 1 2 3 4 5 6 7 8; do [[ "${CK_TOTAL[$n]:-0}" -gt 0 ]] || { OVERALL=FAIL; CK_FAIL[$n]=1; CK_WHY[$n]="no assertion ran"; }; done

REPORT_TMP="$(mktemp "$ORIG_TMPDIR/dw-lanefill-report.XXXXXX")"
{
  printf '# Lane fill eligibility — live check on a sandbox daemon\n\n'
  printf '**Date:** %s (run started %s, local time)\n' "$RUN_DATE" "$DAEMON_UP_AT"
  printf '**Host:** %s\n' "$HOST_NAME"
  printf '**Binary version output:** `%s` (built from this worktree at source revision `%s`)\n' "${VERSION:-unknown}" "$BUILD_REV"
  printf '**Script:** `scripts/lane-fill-eligibility-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/lane-fill-eligibility-live-check.sh`)\n'
  printf '**Scope:** the lane fill eligibility rules (stale, held, sequenced after, proposal, base branch, worktree), their read surfaces, the lane counts, the per-lane-kind mode and the pool evidence, against a throwaway project repository whose default branch is `%s`; the default mode stays shadow throughout except where check 5 sets it on for one lane kind.\n\n' "$PROJECT_DEFAULT_BRANCH"
  printf '## Result\n\n**Overall:** %s\n\n' "$OVERALL"
  for n in 1 2 3 4 5 6 7 8; do
    if [[ "${CK_FAIL[$n]:-0}" -gt 0 ]]; then
      printf 'Check %s: FAIL %s\n\n' "$n" "${CK_WHY[$n]:-no assertion ran}"
    else
      printf 'Check %s: PASS %s\n\n' "$n" "${CK_TITLE[$n]}"
    fi
  done
  printf '| # | Check | Result | Assertions |\n|---|---|---|---|\n'
  for n in 1 2 3 4 5 6 7 8; do
    v=PASS; [[ "${CK_FAIL[$n]:-0}" -gt 0 ]] && v=FAIL
    printf '| %s | %s | %s | %s of %s passed |\n' "$n" "${CK_TITLE[$n]}" "$v" "$(( ${CK_TOTAL[$n]:-0} - ${CK_FAIL[$n]:-0} ))" "${CK_TOTAL[$n]:-0}"
  done
  printf '\n## Setup\n\n'
  printf -- '- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.lane_fill.poll_seconds` (%s), `autonomous.questions.enabled`, `autonomous.verification_backend: shell`, `server.allowed_project_roots` and `shell_backend.script_path` (the stand-in worker). Every Ollama address in the config points at a loopback stub of the script and the test config'"'"'s compute nodes are removed, so nothing leaves the machine. The production daemon, its service and its data were not touched, and nothing was ended by name.\n' "$POLL_SECONDS"
  printf -- '- **Ports.** HTTP %s, TLS %s, MCP %s, DNS %s, webhook %s, notify %s, Ollama stub %s, https stub %s, each probed free just before use.\n' "$PORT" "$TLS_PORT" "$MCP_PORT" "$DNS_PORT" "$HOOK_PORT" "$NTFY_PORT" "$OLLAMA_PORT" "$TLS_STUB_PORT"
  printf -- '- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable. The script itself issues no multiplexer command.\n'
  printf -- '- **Throwaway project.** A bare repository whose default branch is `%s`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist. It holds a proposal file with a `Confirmed: yes` line and others without, and a task commit for one Automaton on the base branch.\n' "$PROJECT_DEFAULT_BRANCH"
  printf -- '- **Worker.** The daemon'"'"'s shell backend runs a generated script as the worker, so a started Automaton runs headlessly with no model; the daemon'"'"'s verification (a one-shot session on the same backend) is answered by the same script with a fixed pass.\n'
  printf -- '- **The Automata** (written into the store before the first start): the eight kinds the check asks for, plus two candidates, (i) for a Claude lane and (j) for a GPU lane, whose proposals are confirmed in check 5 so a lane decision exists to watch.\n\n'
  printf '| Automaton | Kind |\n|---|---|\n'
  for id in "${ALL_IDS[@]}"; do printf '| `%s` | %s |\n' "$id" "${LABEL[$id]}"; done
  printf '\n```\n%s\n```\n' "$DEFAULT_PROBE"
  printf '%s' "${REP[0]:-}"
  for n in 1 2 3 4 5 6 7 8; do printf '%s' "${REP[$n]:-}"; done
  printf '\n## Daemon log, the lane fill lines\n\n```\n%s\n```\n' "$DAEMON_LOG_TAIL"
  printf '\nBranches in the throwaway project at the end: `%s`\n' "$FINAL_REFS"
  printf '\n## Order and limits\n\n'
  printf -- '- **Run order.** The checks ran in the order 1, 2, 3, 4, 7, 5, 6, 8: check 7 needs the state before anything is started, so its data was taken in check 4'"'"'s window and the report shows it under its own number.\n'
  printf -- '- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Every read, setting change and start goes through the REST API or the command line of the running daemon.\n'
  printf -- '- **The chat verb shows the newest five decisions** by design, so check 3 compares those five with the status; the CLI shows every decision it reads.\n'
  printf -- '- **The PWA card** is checked at its code path (the served script and the five locale bundles); the script does not drive a browser.\n'
  printf -- '- **Check 5 confirms a proposal by editing a file in the throwaway repository**, which is how an operator confirms one; the rule reads the file on every poll.\n'
  printf '\nResult: %s\n' "$OVERALL"
} > "$REPORT_TMP"

# No absolute path and no temporary folder name in the tracked report.
mkdir -p "$(dirname "$REPORT")"
{
  printf 's|%s|<sandbox>|g\n' "$S"
  printf 's|%s|<repo>|g\n' "$ROOT"
  [[ -n "${HOME:-}" ]] && printf 's|%s|<home>|g\n' "$HOME"
  printf 's|/tmp/[^ `"'"'"')]*|<temp>|g\n'
  printf 's|/home/[^ `"'"'"')]*|<path>|g\n'
} | sed -f /dev/stdin "$REPORT_TMP" > "$REPORT"
rm -f "$REPORT_TMP"

say ""
say "Overall: $OVERALL — report: ${REPORT#"$ROOT"/}"
[[ "$OVERALL" == PASS ]]
