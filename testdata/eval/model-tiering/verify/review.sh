#!/usr/bin/env bash
# Eval cell verifier — review task (verifier role).
# Usage: review.sh <cell-dir>. Exit 0 only if <cell-dir>/review-verdict.json is
# valid JSON and each candidate's verdict matches ../review/expected.json
# (resolved relative to this script, not cwd). Cell-visible candidate names are
# mapped back to fixture names via ../review/cell-names.json; verdict values
# may be "pass"/"fail" strings or {"verdict": "...", "reasons": [...]} objects.
set -euo pipefail
echo "[$(date '+%H:%M:%S')] start review verify"
CELL=${1:?usage: review.sh <cell-dir>}
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
EXP="$HERE/../review/expected.json"
NAMES="$HERE/../review/cell-names.json"
V="$CELL/review-verdict.json"
[ -s "$V" ] || { echo "FAIL: $V missing or empty"; exit 1; }
python3 -I - "$V" "$EXP" "$NAMES" <<'PY'
import json, sys
v_path, exp_path, names_path = sys.argv[1:4]
try:
    got = json.load(open(v_path))
except Exception as e:
    print(f"FAIL: review-verdict.json is not valid JSON: {e}"); sys.exit(1)
if not isinstance(got, dict):
    print("FAIL: review-verdict.json must be a JSON object"); sys.exit(1)
expected = json.load(open(exp_path))
names = json.load(open(names_path))
norm = {}
for k, val in got.items():
    fixture = names.get(k, k)
    verdict = val.get("verdict") if isinstance(val, dict) else val
    norm[fixture] = str(verdict).strip().lower() if verdict is not None else None
bad = 0
for fixture, want in expected.items():
    have = norm.get(fixture)
    if have != want:
        print(f"FAIL: {fixture}: verdict {have!r}, expected {want!r}"); bad = 1
    else:
        print(f"ok: {fixture}: {have}")
sys.exit(bad)
PY
echo "RESULT: PASS"
echo "[$(date '+%H:%M:%S')] done review verify"
