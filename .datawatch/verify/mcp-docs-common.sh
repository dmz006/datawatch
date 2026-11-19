#!/bin/sh
# Sourced helper: changed-file check against the merge base, cumulative over the whole plan.
# Skipped when MCP_DOCS_SKIP_SET_CHECK=1 (the final gate sets nothing; the list is already cumulative).
mcp_docs_set_check() {
  [ "${MCP_DOCS_SKIP_SET_CHECK:-0}" = "1" ] && return 0
  base=$(git merge-base HEAD main 2>/dev/null || echo HEAD)
  { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u | while IFS= read -r f; do
    case "$f" in
      docs/mcp.md|docs/cursor-mcp.md) ;;
      internal/server/web/docs/*) ;;
      internal/mcp/docs_coverage_test.go) ;;
      internal/mcp/testdata/mcp-docs-undocumented-baseline.txt) ;;
      .datawatch/verify/mcp-docs-rows.sh|.datawatch/verify/mcp-docs-check.sh|.datawatch/verify/mcp-docs-final-gate.sh|.datawatch/verify/mcp-docs-common.sh) ;;
      docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*|docs/plans/*) ;;
      CHANGELOG.md|AGENT.md|worker-starting-context.md|.datawatch/*) ;;
      *) echo "unexpected changed file: $f"; return 1 ;;
    esac
  done
}
