#!/usr/bin/env bash
# Eval cell verifier — scripting task (tracker git-growth repro harness).
# Usage: scripting.sh <cell-dir>. Exit 0 only if the cell satisfies specs/scripting.md.
# Adapted from .datawatch/verify/tracker-growth-repro.sh.
set -euo pipefail
echo "[$(date '+%H:%M:%S')] start scripting verify"
CELL=${1:?usage: scripting.sh <cell-dir>}
cd "$CELL"
S=scripts/repro-tracker-git-growth.sh
[ -f "$S" ] || { echo "FAIL: $S missing"; exit 1; }
[ -x "$S" ] || { echo "FAIL: $S not executable"; exit 1; }
bash -n "$S"
if command -v shellcheck >/dev/null 2>&1; then shellcheck -S warning "$S"; fi
# No hardcoded system-binary directories: tools must be resolved via $PATH.
# Only a line-1 shebang may contain such a path.
hard=$(grep -nE '(^|[^[:alnum:]_.~}-])/(usr/)?(local/)?s?bin/' "$S" | grep -vE '^1:#!' || true)
[ -z "$hard" ] || { echo "FAIL: hardcoded system-binary path(s):"; echo "$hard"; exit 1; }
out=$(mktemp -d)
trap 'rm -rf -- "${out:?}"' EXIT
# Must refuse a workdir under $HOME/.datawatch (exercised with a fake HOME so a
# broken harness can never touch the real one).
mkdir -p "$out/fakehome/.datawatch"
if HOME="$out/fakehome" "$S" --workdir "$out/fakehome/.datawatch/x" --commits 2 --chunk-kb 1 >"$out/refuse.txt" 2>&1; then
  echo "FAIL: harness did not refuse a workdir under \$HOME/.datawatch"; exit 1
fi
if [ -n "$(find "$out/fakehome/.datawatch" -name .git -print -quit)" ]; then
  echo "FAIL: harness created a repo under \$HOME/.datawatch despite refusing"; exit 1
fi
echo "[$(date '+%H:%M:%S')] run harness at --commits 60 --chunk-kb 256"
runner=()
if command -v timeout >/dev/null 2>&1; then runner=(timeout 600); fi
"${runner[@]}" "$S" --workdir "$out/work" --commits 60 --chunk-kb 256 | tee "$out/report.txt"
grep -q 'RESULT: PASS' "$out/report.txt" || { echo "FAIL: repro harness did not report RESULT: PASS"; exit 1; }
echo "RESULT: PASS"
echo "[$(date '+%H:%M:%S')] done scripting verify"
