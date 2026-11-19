#!/bin/sh
# Final gate: rows present, embedded copy synced, coverage test and the mcp package tests pass.
set -e
cd "$(git rev-parse --show-toplevel)"
make sync-docs >/dev/null
. .datawatch/verify/mcp-docs-common.sh
mcp_docs_set_check
sh .datawatch/verify/mcp-docs-check.sh
env -u TMUX -u TMUX_TMPDIR go build ./internal/mcp/ ./internal/server/
env -u TMUX -u TMUX_TMPDIR go test ./internal/mcp/ -count=1
echo ok
