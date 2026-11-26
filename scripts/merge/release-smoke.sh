#!/usr/bin/env bash
# The release smoke test this merge needs, against a binary built from the
# commit about to be pushed, on the script's own sandbox daemon.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_tool go
# Always the script's own sandbox daemon: never a daemon already running.
unset DW_BASE DW_BASE_TOKEN SMOKE_PORT SMOKE_TLS_PORT
mode=$(bash scripts/merge/smoke-mode.sh)
echo "release smoke: version ${MERGE_RELEASE_VERSION:-unknown}, mode $mode"
if [[ "$mode" == skip ]]; then
    echo "release smoke: not needed, this merge changes only documentation"
    exit 0
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
make sync-docs docs-index
go build -o "$work/datawatch" ./cmd/datawatch/
only=()
if [[ "$mode" == targeted ]]; then
    only=("SMOKE_ONLY=${MERGE_SMOKE_SECTIONS:-1,2,3,4}")
fi
env "${only[@]}" DATAWATCH_BIN="$work/datawatch" nice -n 10 timeout 2400 bash scripts/release-smoke.sh
