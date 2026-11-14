#!/bin/bash
# Test: scripts/release-smoke.sh never increments a counter it does not
# initialise. The script runs under `set -u`, so `X=$((X+1))` on a variable that
# is never assigned aborts the whole smoke with "unbound variable".
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SMOKE="${RS:-$HERE/../release-smoke.sh}"
[[ -f "$SMOKE" ]] || { echo "FAIL: $SMOKE not found" >&2; exit 1; }
FAILURES=0
bash -n "$SMOKE" || { echo "FAIL: bash -n release-smoke.sh" >&2; FAILURES=$((FAILURES+1)); }
# Every NAME incremented as NAME=$((NAME+1)) or ((NAME++)) / ((NAME+=n)).
names=$( { grep -oE '\b([A-Za-z_][A-Za-z0-9_]*)=\$\(\(\s*\$?\{?\1\}?\s*\+' "$SMOKE" | sed -E 's/=.*//' || true;
           grep -oE '\(\(\s*[A-Za-z_][A-Za-z0-9_]*\s*(\+\+|\+=)' "$SMOKE" | sed -E 's/^\(\(\s*//; s/\s*(\+\+|\+=).*//' || true; } | sort -u)
for n in $names; do
  # An initialisation is a plain NAME=<non-arithmetic> assignment (also local/declare).
  if ! grep -qE "^[[:space:]]*(local[[:space:]]+|declare[[:space:]]+(-[a-zA-Z]+[[:space:]]+)?|export[[:space:]]+)?${n}=([^\$]|\\\$[^(]|$)|\\b${n}=\\\$\\{" "$SMOKE"; then
    echo "FAIL: $n is incremented in release-smoke.sh but never initialised" >&2
    FAILURES=$((FAILURES+1))
  else
    echo "PASS: $n is initialised"
  fi
done
if grep -q 'SMOKE_PASS' "$SMOKE"; then echo "FAIL: stray SMOKE_PASS reference" >&2; FAILURES=$((FAILURES+1)); fi
[[ $FAILURES -eq 0 ]] || { echo "$FAILURES failure(s)" >&2; exit 1; }
echo "all passed"
