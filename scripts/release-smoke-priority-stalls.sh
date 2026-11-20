#!/usr/bin/env bash
# release-smoke-priority-stalls.sh — standalone smoke test for the
# priority-stalls surface (priority-stalled work that needs attention).
#
# The unit tests cover the Go level.  This script walks the three operator
# surfaces, exactly once each:
#   REST  GET /api/autonomous/priority-stalls → 200 with a `stalls` array,
#         and a non-GET on the same route → 405 (read-only).
#   MCP   a spawned stdio `datawatch mcp` lists the `priority_stalls` tool.
#   CLI   `datawatch autonomous priority-stalls --json` prints a `stalls`
#         array.
#
# Every probe is read-only: no session is started, no task runs, and nothing
# here wakes the supervisor.
#
# Env (same conventions as the sibling release smoke scripts):
#   DATAWATCH_BASE      daemon base URL (default https://localhost:8443)
#   DATAWATCH_TOKEN     bearer token for REST + the CLI (optional)
#   DATAWATCH_INSECURE  1 = skip TLS verification (default 1)
#   DW_BIN              datawatch binary for the MCP + CLI probes.
#                       Default: `datawatch` on the PATH; neither present →
#                       those two probes skip and the script exits 2.
#   DW_BIN_REQUIRED     1 = never auto-discover a binary (release-smoke
#                       local mode sets this when it has no build to test,
#                       so an unrelated installed copy can never be probed)
#   DW_CONFIG           config the spawned `datawatch mcp` and the CLI load
#                       (`--config`); release-smoke local mode passes the
#                       smoke daemon's config; standalone may omit it
#   DATAWATCH_CLI_URL   optional -u target for the CLI probe (defaults to
#                       DATAWATCH_BASE)
#
# Exit codes: 0 = all probes pass, 1 = a genuine failure
#             (a route answered a wrong code / malformed body / missing
#             tool), 2 = skipped (daemon unreachable, the endpoint absent
#             or disabled — 404/503 — or no datawatch binary).

set -uo pipefail

BASE="${DATAWATCH_BASE:-https://localhost:8443}"
TOKEN="${DATAWATCH_TOKEN:-}"
INSECURE="${DATAWATCH_INSECURE:-1}"
if [[ "$INSECURE" == "1" ]]; then
  curl_args=(--silent --show-error --max-time 10 --insecure)
else
  curl_args=(--silent --show-error --max-time 10)
fi
if [[ -n "$TOKEN" ]]; then
  auth_args=(-H "Authorization: Bearer $TOKEN")
else
  auth_args=()
fi

# ---------- resolve the datawatch binary for the MCP + CLI probes ----------
DW_BIN="${DW_BIN:-}"
if [[ -z "$DW_BIN" && "${DW_BIN_REQUIRED:-0}" != "1" ]]; then
  if [[ -n "${REPO_DIR:-}" && -x "${REPO_DIR}/datawatch" ]]; then
    DW_BIN="${REPO_DIR}/datawatch"
  else
    DW_BIN="$(command -v datawatch 2>/dev/null || true)"
  fi
fi

failed=0
any_skip=0

fail() {
  echo "FAIL: $1"
  failed=1
}
skipped() {
  echo "SKIP: $1"
  any_skip=1
}

# ---------------------------------------------------------------------------
# 0. Daemon reachable? (not present → cannot probe anything → skip)
# ---------------------------------------------------------------------------
if ! curl "${curl_args[@]}" "$BASE/api/health" >/dev/null 2>&1; then
  echo "SKIP: $BASE unreachable — cannot probe the priority-stalls surface"
  exit 2
fi

REST_UP=0     # 1 once the GET probe answered 200 with a stalls array
REST_ABSENT=0 # 1 when the daemon answered 404/503: the endpoint is absent or
              # disabled in this build, so the 405, MCP and CLI probes skip
              # with it (the binary under test came from the same build)
REST_N=0
PS_POST="000"

# ---------------------------------------------------------------------------
# Probe 1: GET /api/autonomous/priority-stalls → 200 + `stalls` array.
# 404/503 = endpoint absent or disabled in this build → skip (the 405, MCP
# and CLI probes ride on the route existing, so they skip too).
# ---------------------------------------------------------------------------
PS_BODY=$(curl "${curl_args[@]}" "${auth_args[@]}" -w '\n%{http_code}' "$BASE/api/autonomous/priority-stalls" 2>/dev/null || true)
PS_CODE=$(echo "$PS_BODY" | tail -n1)
PS_JSON=$(echo "$PS_BODY" | sed '$d')

if [[ "$PS_CODE" == "404" || "$PS_CODE" == "503" ]]; then
  REST_ABSENT=1
  skipped "REST — GET /api/autonomous/priority-stalls: HTTP $PS_CODE (endpoint absent or disabled); the 405, MCP and CLI probes skip with it"
elif [[ "$PS_CODE" == "200" ]] && echo "$PS_JSON" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert isinstance(d, dict), d
assert isinstance(d["stalls"], list), d
' 2>/dev/null; then
  REST_UP=1
  REST_N=$(echo "$PS_JSON" | python3 -c 'import json, sys; print(len(json.load(sys.stdin)["stalls"]))' 2>/dev/null || echo "?")
  echo "OK: REST — GET /api/autonomous/priority-stalls: HTTP 200 with a stalls array ($REST_N entries)"
else
  fail "REST — GET /api/autonomous/priority-stalls: expected HTTP 200 with a stalls array, got HTTP ${PS_CODE:-?}: $(echo "$PS_JSON" | head -c 300)"
fi

# ---------------------------------------------------------------------------
# Probe 2: the route is read-only — a POST on it answers 405.  Only probed
# when the route exists (200 above); 404/503 already skipped it.
# ---------------------------------------------------------------------------
if [[ "$REST_UP" -eq 1 ]]; then
  PS_POST=$(curl "${curl_args[@]}" "${auth_args[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/autonomous/priority-stalls" 2>/dev/null || true)
  if [[ "$PS_POST" == "405" ]]; then
    echo "OK: REST — POST /api/autonomous/priority-stalls: HTTP 405 (read-only route)"
  else
    fail "REST — POST /api/autonomous/priority-stalls: expected HTTP 405 (read-only), got ${PS_POST:-no response}"
  fi
fi

# ---------------------------------------------------------------------------
# Probe 3: MCP — a spawned stdio `datawatch mcp` must list priority_stalls
# in its tools/list.  The daemon answers the same route probe 1 hit, so an
# absent endpoint there means the binary under test is the same build and
# skips with it; no binary at all → skip (same as the other smoke scripts'
# stdio probe).
# ---------------------------------------------------------------------------
if [[ "$REST_ABSENT" -eq 1 ]]; then
  skipped "MCP — the daemon has no priority-stalls route (HTTP $PS_CODE), so the tool-list probe (same build) skips"
elif [[ -z "$DW_BIN" ]]; then
  skipped "MCP — no datawatch binary (set DW_BIN=path to probe priority_stalls in tools/list)"
else
  MCP_RAW=$(mktemp)
  MCP_ERR=$(mktemp)
  MCP_ARGS=()
  [[ -n "${DW_CONFIG:-}" ]] && MCP_ARGS=(--config "$DW_CONFIG")
  {
    printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"release-smoke-priority-stalls","version":"0"}}}'
    printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized"}'
    printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
    sleep 2
  } | (command -v timeout >/dev/null 2>&1 && timeout 90 "$DW_BIN" mcp ${MCP_ARGS[@]+"${MCP_ARGS[@]}"} || "$DW_BIN" mcp ${MCP_ARGS[@]+"${MCP_ARGS[@]}"}) > "$MCP_RAW" 2>"$MCP_ERR"
  if python3 - "$MCP_RAW" <<'PY' 2>/dev/null
import sys
from json import JSONDecoder

text = open(sys.argv[1], encoding="utf-8", errors="replace").read()
dec = JSONDecoder()
i, n, found = 0, len(text), False
while i < n:
    while i < n and text[i].isspace():
        i += 1
    if i >= n:
        break
    msg, i = dec.raw_decode(text, i)
    if msg.get("id") != 2:
        continue
    found = True
    if "error" in msg:
        raise SystemExit(1)
    names = [t.get("name") for t in (msg["result"].get("tools") or [])]
    assert "priority_stalls" in names, names[:40]
    break
sys.exit(0 if found else 1)
PY
  then
    echo "OK: MCP — the stdio tool list contains priority_stalls"
  else
    fail "MCP — priority_stalls is not in the stdio tools/list response: $(head -c 300 "$MCP_RAW" 2>/dev/null | tr '\n' ' ' | tail -c 300 || head -c 300 "$MCP_ERR" 2>/dev/null)"
  fi
  rm -f "$MCP_RAW" "$MCP_ERR"
fi

# ---------------------------------------------------------------------------
# Probe 4: CLI — `datawatch autonomous priority-stalls --json` prints a
# `stalls` array.  (The flag order is exactly `autonomous priority-stalls
# [--json]`; the daemon answers the same route as probe 1.)  No binary →
# skip; the route itself absent/disabled in this build → skip (it is the
# same daemon route probe 1 found missing, not a CLI defect to fail on
# here).
# ---------------------------------------------------------------------------
if [[ "$REST_ABSENT" -eq 1 ]]; then
  skipped "CLI — the REST route is absent/disabled (HTTP $PS_CODE), so the CLI probe (same daemon route) skips"
elif [[ -z "$DW_BIN" ]]; then
  skipped "CLI — no datawatch binary (set DW_BIN=path to probe the autonomous priority-stalls command)"
else
  CLI_URL="${DATAWATCH_CLI_URL:-$BASE}"
  CLI_ARGS=(-u "$CLI_URL")
  [[ -n "${DW_CONFIG:-}" ]] && CLI_ARGS=(--config "$DW_CONFIG" -u "$CLI_URL")
  INSECURE_ENV=()
  [[ "$INSECURE" == "1" && -z "${DW_CONFIG:-}" ]] && INSECURE_ENV=(DW_BASE_INSECURE=1)
  PS_CLI_OUT=$(env DATAWATCH_TOKEN="$TOKEN" ${INSECURE_ENV[@]+"${INSECURE_ENV[@]}"} "$DW_BIN" autonomous priority-stalls --json "${CLI_ARGS[@]}" 2>&1)
  PSI_CLI_RC=$?
  if [[ $PSI_CLI_RC -eq 0 ]] && echo "$PS_CLI_OUT" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert isinstance(d, dict), d
assert isinstance(d["stalls"], list), d
' 2>/dev/null; then
    echo "OK: CLI — datawatch autonomous priority-stalls --json prints a stalls array"
  else
    fail "CLI — datawatch autonomous priority-stalls --json (rc=$PSI_CLI_RC): $(echo "$PS_CLI_OUT" | head -c 300)"
  fi
fi

# ---------------------------------------------------------------------------
if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
if [[ "$any_skip" -ne 0 ]]; then
  echo "SKIP: priority-stalls smoke — some probes skipped (see above), none failed"
  exit 2
fi
echo "OK: all priority-stalls probes passed (REST 200 + 405, MCP priority_stalls, CLI stalls array)"
exit 0
