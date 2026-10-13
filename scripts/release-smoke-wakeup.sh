#!/usr/bin/env bash
# v5.26.71 — Standalone smoke for the L0-L5 wake-up bundle composer.
#
# The unit tests at internal/memory/layers_recursive_test.go cover the
# Go-level composition. This wrapper exercises the REST surface
# (GET /api/memory/wakeup) so we know an actual agent's bootstrap
# would receive the same bytes.
#
# Probes:
#   1. L0+L1 only (no agent_id)            — base wake-up bundle
#   2. L0+L1+L4 (parent_agent_id supplied) — adds parent context
#   3. L0+L1+L4+L5 (self+parent)           — adds sibling visibility
#
# Skips cleanly when:
#   - daemon unreachable
#   - /api/memory/wakeup returns 503 (memory not enabled)
#
# Exit code 0 = pass, 1 = real failure, 2 = skip.

set -uo pipefail

BASE="${DATAWATCH_BASE:-https://localhost:8443}"
TOKEN="${DATAWATCH_TOKEN:-}"
INSECURE="${DATAWATCH_INSECURE:-1}"

curl_args=(--silent --show-error --max-time 10)
[[ "$INSECURE" == "1" ]] && curl_args+=(-k)
[[ -n "$TOKEN" ]] && curl_args+=(-H "Authorization: Bearer $TOKEN")

if ! curl "${curl_args[@]}" "$BASE/api/health" >/dev/null 2>&1; then
  echo "SKIP: daemon unreachable at $BASE"
  exit 2
fi

probe() {
  local label="$1"; shift
  local resp
  resp=$(curl "${curl_args[@]}" "$BASE/api/memory/wakeup?$1")
  local code=$?
  if [[ $code -ne 0 ]]; then
    echo "FAIL: $label — curl rc=$code"
    return 1
  fi
  if [[ "$resp" == *"memory not enabled"* ]]; then
    echo "SKIP: $label — memory subsystem disabled"
    return 2
  fi
  # Validate response shape — must have {bundle, length, has_l4_l5}.
  if ! python3 -c "
import json, sys
d = json.loads(sys.argv[1])
assert 'bundle' in d, 'missing bundle key'
assert 'length' in d, 'missing length key'
assert 'has_l4_l5' in d, 'missing has_l4_l5 key'
assert isinstance(d['length'], int), 'length not int'
" "$resp" 2>/dev/null; then
    echo "FAIL: $label — bad response shape"
    echo "  raw: ${resp:0:200}"
    return 1
  fi
  local len has
  len=$(python3 -c "import json,sys;print(json.loads(sys.argv[1])['length'])" "$resp")
  has=$(python3 -c "import json,sys;print(json.loads(sys.argv[1])['has_l4_l5'])" "$resp")
  echo "PASS: $label — bundle len=$len has_l4_l5=$has"
  return 0
}

PROJ="${DATAWATCH_PROJECT_DIR:-$HOME}"
proj_q="project_dir=$(python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))' "$PROJ")"

# 2026-10-13 — found running release-smoke.sh in external-daemon mode
# against a real memory-disabled chart deployment for the first time:
# `probe ... || failed=1` conflated a clean skip (return 2, "memory
# subsystem disabled" — not a bug, just not configured) with a real
# failure (return 1), so the script always exited 1 whenever memory
# was simply off, even though every individual probe correctly printed
# "SKIP: ...". Track skip and fail separately: any real failure (1)
# still exits 1; all-skip (2, no real failures) exits 2 (clean skip);
# all-pass exits 0.
failed=0
any_skip=0
probe "L0+L1 base"            "$proj_q"; rc=$?
[[ $rc -eq 1 ]] && failed=1
[[ $rc -eq 2 ]] && any_skip=1
probe "L0+L1+L4 with parent"  "$proj_q&parent_agent_id=smoke-parent&parent_namespace=smoke-ns"; rc=$?
[[ $rc -eq 1 ]] && failed=1
[[ $rc -eq 2 ]] && any_skip=1
probe "L0+L1+L4+L5 self+parent" "$proj_q&agent_id=smoke-self&parent_agent_id=smoke-parent"; rc=$?
[[ $rc -eq 1 ]] && failed=1
[[ $rc -eq 2 ]] && any_skip=1

if [[ $failed -ne 0 ]]; then
  exit 1
fi
if [[ $any_skip -ne 0 ]]; then
  echo "SKIP: one or more wake-up bundle probes skipped (memory subsystem disabled)"
  exit 2
fi
echo "OK: all wake-up bundle probes passed"
