#!/bin/sh
# Verify the two archived-unmerged tools have rows in both MCP docs tables and the embedded copy matches.
set -e
cd "$(git rev-parse --show-toplevel)"
make sync-docs >/dev/null
. .datawatch/verify/mcp-docs-common.sh
mcp_docs_set_check
for f in docs/mcp.md docs/cursor-mcp.md internal/server/web/docs/mcp.md internal/server/web/docs/cursor-mcp.md; do
  [ -f "$f" ] || { echo "missing $f"; exit 1; }
  for t in archived_unmerged_list archived_unmerged_carry_forward; do
    grep -q "\`$t\`" "$f" || { echo "$f lacks $t"; exit 1; }
  done
done
cmp -s docs/mcp.md internal/server/web/docs/mcp.md || { echo "embedded mcp.md stale"; exit 1; }
cmp -s docs/cursor-mcp.md internal/server/web/docs/cursor-mcp.md || { echo "embedded cursor-mcp.md stale"; exit 1; }
grep -q 'force' docs/cursor-mcp.md && grep -q 'actor' docs/cursor-mcp.md || { echo "force and actor missing from cursor-mcp.md"; exit 1; }
sh scripts/check-docs-sync.sh
echo ok
