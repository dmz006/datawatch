#!/usr/bin/env bash
# Every Go test, with bounded memory and a bounded number of packages at once,
# and no way to reach a terminal server of the operator's.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_tool go
make sync-docs docs-index
export GOMEMLIMIT="${MERGE_GOMEMLIMIT:-2GiB}"
log=$(mktemp)
trap 'rm -f "$log"' EXIT
if nice -n 10 go test -p "$(merge_workers)" -timeout 35m ./... 2>&1 | tee "$log"; then
    exit 0
fi
# A run record keeps only the end of a check's output: name what failed last.
echo "== failed tests and packages"
grep -E '^ *--- FAIL|^FAIL' "$log" | head -40
exit 1
