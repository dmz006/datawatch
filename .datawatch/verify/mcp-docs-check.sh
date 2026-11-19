#!/bin/sh
# Verify the MCP docs coverage test exists and passes, and the docs rows are present.
set -e
cd "$(git rev-parse --show-toplevel)"
make sync-docs >/dev/null
. .datawatch/verify/mcp-docs-common.sh
mcp_docs_set_check
[ -f internal/mcp/docs_coverage_test.go ] || { echo "missing internal/mcp/docs_coverage_test.go"; exit 1; }
if [ -n "$(gofmt -l internal/mcp/docs_coverage_test.go)" ]; then echo "gofmt needed"; exit 1; fi
env -u TMUX -u TMUX_TMPDIR go test ./internal/mcp/ -run 'TestMCPToolsDocumentedInMCPMd' -count=1
sh .datawatch/verify/mcp-docs-rows.sh
echo ok
