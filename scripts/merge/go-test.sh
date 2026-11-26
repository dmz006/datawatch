#!/usr/bin/env bash
# Every Go test, with bounded memory and a bounded number of packages at once,
# and no way to reach a terminal server of the operator's.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_tool go
make sync-docs docs-index
export GOMEMLIMIT="${MERGE_GOMEMLIMIT:-2GiB}"
nice -n 10 go test -p "$(merge_workers)" -timeout 35m ./...
