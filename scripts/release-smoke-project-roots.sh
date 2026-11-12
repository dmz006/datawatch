#!/usr/bin/env bash
# release-smoke-project-roots.sh — end-to-end smoke for the allowed project roots
#
# The daemon accepts a session project_dir only when it lies under an allowed
# project root (by default the session workspace root; more roots come from
# server.allowed_project_roots). This drives the real start endpoint of a
# SANDBOX daemon and asserts both sides of that guard.
#
# What it proves:
#   (a) a one-shot session whose project_dir is a fresh folder under the
#       workspace root is accepted (HTTP 200/201 with a full_id);
#   (b) a session whose project_dir is a not-yet-existing child of a folder
#       outside every allowed root is refused with HTTP 400 and a body naming
#       'outside the allowed project roots', and the refusal did not create
#       the folder.
#
# SAFETY: this script NEVER points at a production port. The target daemon
# base URL must resolve to loopback (127.0.0.1 / localhost / ::1). Anything
# else is refused outright, before any network call. It runs no tmux command;
# the one session it starts is stopped through POST /api/sessions/kill.
#
# Usage:
#   scripts/release-smoke-project-roots.sh
#
# Env vars:
#   DW_BASE               sandbox daemon base URL (default: http://127.0.0.1:9090)
#   DW_BASE_TOKEN         bearer token for the sandbox daemon (optional)
#   DW_BASE_INSECURE      set to 1 to accept a self-signed sandbox certificate
#   DW_WORKSPACE_ROOT     the inside root (default: session.workspace_root from
#                         GET /api/config, else $HOME/workspace)
#   DW_WORKTREE_BASE      PRD worktree base the outside folder must avoid
#                         (default: $HOME/.datawatch/prd-worktrees)
#   DW_LLM_BACKEND        LLM backend for the accepted session
#                         (default: opencode-acp — free, local; set
#                         DW_LLM_BACKEND=claude-code or DW_MAJOR=1 to
#                         spend API quota instead)
#   DW_MAJOR              set to 1 to allow claude-code when the default
#                         free backend is unavailable (cost gate, matches
#                         release-smoke.sh's convention)
#   KEEP_TEST_DIR         set to 1 to keep the scratch folders for inspection
#                         (default: deleted on exit)
#
# Exit codes:
#   0 — all checks passed (or cleanly skipped: no sandbox daemon, missing
#       tooling, no usable folders, or no LLM backend and cost gate closed)
#   1 — one or more checks failed
#   2 — refused to run at all (non-loopback target)
set -euo pipefail
unset TMUX TMUX_TMPDIR

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

summary() {
  printf '\n=== RESULTS ===\n'
  printf 'PASS: %d\n' "$PASS_COUNT"
  printf 'FAIL: %d\n' "$FAIL_COUNT"
  printf 'SKIP: %d\n' "$SKIP_COUNT"
}

# Print the skip reason, the summary and leave with success.
skip_all() { # $1 = reason
  skip "$1"
  summary
  printf '%s\n' "ALL SKIPPED — $1"
  exit 0
}

# ---------------------------------------------------------------------------
# Target resolution + sandbox guard
# ---------------------------------------------------------------------------
BASE="${DW_BASE:-http://127.0.0.1:9090}"
BASE="${BASE%/}"

# Host part of the URL. Only the authority (between "://" and the first of
# "/", "?" or "#") is considered, so a query or fragment can never smuggle a
# fake host in. A URL carrying userinfo ("@") or a backslash is refused
# outright: curl and a shell parser can disagree about where its host begins.
# A bracketed IPv6 literal keeps its brackets and is unwrapped afterwards.
refuse() {
  printf '[REFUSED] target daemon is not loopback: %s\n' "$BASE" >&2
  printf 'This smoke only runs against a SANDBOX daemon (127.0.0.1/localhost/::1).\n' >&2
  exit 2
}

case "$BASE" in
  http://*|https://*) : ;;
  *) refuse ;;
esac
AUTH="${BASE#*://}"
AUTH="${AUTH%%[/?#]*}"
case "$AUTH" in
  *@*|*\\*|"") refuse ;;
  \[*) HOST="${AUTH%%]*}"; HOST="${HOST#\[}" ;;
  *)   HOST="${AUTH%%:*}" ;;
esac
HOST=$(printf '%s' "$HOST" | tr '[:upper:]' '[:lower:]')

# Sandbox-only guard: refuse any non-loopback host before touching anything.
case "$HOST" in
  127.0.0.1|localhost|"::1") : ;;
  *) refuse ;;
esac

# ---------------------------------------------------------------------------
# Tooling (resolved by name, never by a literal path)
# ---------------------------------------------------------------------------
CURL=$(command -v curl || true)
PY=$(command -v python3 || true)
MKTEMP=$(command -v mktemp || true)
[[ -n "$CURL" ]]   || skip_all "curl not found — project-roots smoke skipped"
[[ -n "$PY" ]]     || skip_all "python3 not found — project-roots smoke skipped"
[[ -n "$MKTEMP" ]] || skip_all "mktemp not found — project-roots smoke skipped"

TOKEN="${DW_BASE_TOKEN:-}"
CURL_ARGS=(-s --max-time 20)
[[ -n "$TOKEN" ]] && CURL_ARGS+=(-H "Authorization: Bearer $TOKEN")
[[ "${DW_BASE_INSECURE:-0}" == "1" ]] && CURL_ARGS+=(-k)

py() { "$PY" -I "$@"; }

# ---------------------------------------------------------------------------
# Is there a sandbox daemon at all? (clear skip message if not)
# ---------------------------------------------------------------------------
HEALTH=$("$CURL" "${CURL_ARGS[@]}" "$BASE/api/health" 2>/dev/null || echo "")
if ! printf '%s' "$HEALTH" | grep -qiE '"status"\s*:\s*"(ok|healthy|up)"'; then
  printf '  [SKIP] no sandbox datawatch daemon reachable at %s — project-roots smoke skipped\n' "$BASE"
  printf '         Start a sandbox daemon (or point DW_BASE/DW_BASE_TOKEN at one) and re-run.\n'
  SKIP_COUNT=$((SKIP_COUNT + 1))
  summary
  printf '%s\n' "ALL SKIPPED — no sandbox daemon configured"
  exit 0
fi
ok "sandbox daemon reachable at $BASE"

# ---------------------------------------------------------------------------
# Cleanup state (registered before anything is created)
# ---------------------------------------------------------------------------
INSIDE=""
OUTSIDE=""
BODY=""
SID=""

kill_session() { # $1 = session id; echoes the HTTP status
  local payload
  payload=$(py -c 'import json,sys;print(json.dumps({"id": sys.argv[1]}))' "$1")
  "$CURL" "${CURL_ARGS[@]}" -o /dev/null -w '%{http_code}' -X POST \
    -H "Content-Type: application/json" -d "$payload" \
    "$BASE/api/sessions/kill" 2>/dev/null || echo "000"
}

cleanup() {
  local rc=$?
  if [[ -n "$SID" ]]; then
    kill_session "$SID" >/dev/null 2>&1 || true
  fi
  [[ -n "$BODY" ]] && rm -f "$BODY" 2>/dev/null || true
  if [[ "${KEEP_TEST_DIR:-}" == "1" ]]; then
    [[ -n "$INSIDE" || -n "$OUTSIDE" ]] && \
      printf '  (KEEP_TEST_DIR set — left %s %s)\n' "${INSIDE:-}" "${OUTSIDE:-}"
  else
    [[ -n "$INSIDE" ]] && rm -rf "$INSIDE" 2>/dev/null || true
    [[ -n "$OUTSIDE" ]] && rm -rf "$OUTSIDE" 2>/dev/null || true
  fi
  exit $rc
}
trap cleanup EXIT

BODY=$("$MKTEMP")

# post_json <path> <json> — echoes the HTTP status, the body lands in $BODY
post_json() {
  "$CURL" "${CURL_ARGS[@]}" -o "$BODY" -w '%{http_code}' -X POST \
    -H "Content-Type: application/json" -d "$2" "$BASE$1" 2>/dev/null || echo "000"
}

# under <base> <path> — true when path is base or lies below it (both resolved)
under() {
  py -c '
import os, sys
b = os.path.realpath(sys.argv[1])
p = os.path.realpath(sys.argv[2])
try:
    sys.exit(0 if os.path.commonpath([b, p]) == b else 1)
except ValueError:
    sys.exit(1)' "$1" "$2"
}

# ---------------------------------------------------------------------------
# (1) Inside folder under the workspace root
# ---------------------------------------------------------------------------
ROOT="${DW_WORKSPACE_ROOT:-}"
if [[ -z "$ROOT" ]]; then
  ROOT=$("$CURL" "${CURL_ARGS[@]}" "$BASE/api/config" 2>/dev/null | py -c '
import json, os, sys
try:
    d = json.load(sys.stdin)
    print(os.path.expanduser((d.get("session") or {}).get("workspace_root") or ""))
except Exception:
    print("")' 2>/dev/null || echo "")
fi
[[ -n "$ROOT" ]] || ROOT="${HOME:-}/workspace"
ROOT="${ROOT%/}"

if [[ -z "$ROOT" || ! -d "$ROOT" ]]; then
  skip_all "workspace root '${ROOT:-<unset>}' is not a directory — nothing to place the inside folder under"
fi

RND="$$-$RANDOM-$RANDOM"
INSIDE="$ROOT/dw-roots-smoke-$RND"
if ! mkdir "$INSIDE" 2>/dev/null; then
  INSIDE=""
  skip_all "cannot create a folder under workspace root $ROOT"
fi
ok "inside folder ready: $INSIDE"

# ---------------------------------------------------------------------------
# (2) Outside folder
# ---------------------------------------------------------------------------
OUTSIDE=$("$MKTEMP" -d)
WORKTREE_BASE="${DW_WORKTREE_BASE:-${HOME:-}/.datawatch/prd-worktrees}"
if under "$ROOT" "$OUTSIDE"; then
  skip_all "temp folder $OUTSIDE lies under the inside root $ROOT — it would not be outside the allowed roots"
fi
if under "$WORKTREE_BASE" "$OUTSIDE"; then
  skip_all "temp folder $OUTSIDE lies under the worktree base $WORKTREE_BASE — it would not be outside the allowed roots"
fi
ok "outside folder ready: $OUTSIDE"

# ---------------------------------------------------------------------------
# (3) LLM backend (cost-gated, like release-smoke-automatic-commit.sh)
# ---------------------------------------------------------------------------
backend_available() { # $1 = mode name; echoes yes/no
  "$CURL" "${CURL_ARGS[@]}" "$BASE/api/backends" 2>/dev/null | py -c '
import json, sys
name = sys.argv[1]
d = json.load(sys.stdin)
for b in d.get("llm", []):
    if b.get("name") == name:
        print("yes" if b.get("available") else "no")
        break
else:
    print("no")' "$1" 2>/dev/null || echo "no"
}

BACKEND="${DW_LLM_BACKEND:-}"
if [[ -z "$BACKEND" ]]; then
  BACKEND="opencode-acp"
  if [[ "$(backend_available opencode)" != "yes" ]]; then
    if [[ "${DW_MAJOR:-0}" == "1" && "$(backend_available claude-code)" == "yes" ]]; then
      BACKEND="claude-code"
    else
      BACKEND=""
    fi
  fi
else
  PROBE="$BACKEND"
  [[ "$PROBE" == "opencode-acp" ]] && PROBE="opencode"
  [[ "$(backend_available "$PROBE")" == "yes" ]] || BACKEND=""
fi
if [[ -z "$BACKEND" ]]; then
  printf '         Re-run with DW_MAJOR=1 (allows claude-code, spends quota) or DW_LLM_BACKEND=<name>.\n'
  skip_all "no available LLM backend on the sandbox — project-roots smoke skipped"
fi
ok "worker backend: $BACKEND"

TASK="Print exactly: DATAWATCH_COMPLETE: project roots smoke"

start_payload() { # $1 = project_dir $2 = session name
  py -c '
import json, sys
print(json.dumps({
    "task": sys.argv[1],
    "project_dir": sys.argv[2],
    "backend": sys.argv[3],
    "name": sys.argv[4],
    "one_shot": True,
}))' "$TASK" "$1" "$BACKEND" "$2"
}

# ===========================================================================
H "(4) a project_dir inside the allowed root is accepted"
# ===========================================================================
CODE=$(post_json /api/sessions/start "$(start_payload "$INSIDE" smoke-roots-inside)")
SID=$(py -c '
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("full_id", ""))
except Exception:
    print("")' "$BODY" 2>/dev/null || echo "")
if [[ ( "$CODE" == "200" || "$CODE" == "201" ) && -n "$SID" ]]; then
  ok "inside project_dir accepted (HTTP $CODE, session $SID)"
else
  SID=""
  ko "inside project_dir not accepted (HTTP $CODE, body: $(head -c 300 "$BODY" 2>/dev/null || true))"
fi

# ===========================================================================
H "(5) a project_dir outside every allowed root is refused and not created"
# ===========================================================================
REFUSED="$OUTSIDE/dw-refused-$RND"
CODE=$(post_json /api/sessions/start "$(start_payload "$REFUSED" smoke-roots-outside)")
if [[ "$CODE" == "400" ]] && grep -qF 'outside the allowed project roots' "$BODY" 2>/dev/null; then
  ok "outside project_dir refused (HTTP 400, 'outside the allowed project roots')"
else
  ko "outside project_dir not refused as expected (HTTP $CODE, body: $(head -c 300 "$BODY" 2>/dev/null || true))"
  # An unexpected acceptance started a session: stop it through the API.
  EXTRA=$(py -c '
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("full_id", ""))
except Exception:
    print("")' "$BODY" 2>/dev/null || echo "")
  [[ -n "$EXTRA" ]] && kill_session "$EXTRA" >/dev/null 2>&1 || true
fi
if [[ ! -e "$REFUSED" ]]; then
  ok "refused folder was not created: $REFUSED"
else
  ko "refused folder exists after the refusal: $REFUSED"
fi

# ===========================================================================
H "(6) stop the accepted session through the daemon"
# ===========================================================================
if [[ -z "$SID" ]]; then
  skip "no accepted session to stop"
else
  CODE=$(kill_session "$SID")
  case "$CODE" in
    200|201|202|204) ok "session $SID stopped via /api/sessions/kill (HTTP $CODE)"; SID="" ;;
    404)             skip "session $SID already gone before kill (HTTP 404)"; SID="" ;;
    *)               ko "kill of session $SID answered HTTP $CODE" ;;
  esac
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
summary
if [[ $FAIL_COUNT -gt 0 ]]; then
  printf '%s\n' "FAILED"
  exit 1
else
  printf '%s\n' "ALL PASSED — project roots accept the workspace folder and refuse the outside one"
  exit 0
fi
