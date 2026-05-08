#!/usr/bin/env bash
# scripts/check-curated-howtos.sh — enforces the
# "Docs-as-MCP Currency" rule (BL274 closure, AGENT.md).
#
# The 22 curated howtos under docs/howto/ MUST carry a YAML front-matter
# `exec_steps:` block whose every `tool:` references a real registered MCP
# tool. Drift = silent breakage of `docs_apply` execute mode (see the
# v6.18.0 chunker bug for the kind of leak this category produces).
#
# Run as part of release-smoke.sh; failing exit code blocks the release.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Curated set per the BL274 plan doc (Sprint 1+2+3 = 22 howtos).
CURATED=(
    setup-and-install identity-and-telos secrets-manager council-mode
    daemon-operations profiles comm-channels autonomous-planning
    autonomous-review-approve algorithm-mode container-workers
    tailscale-mesh skills-sync federated-observer sessions-deep-dive
    cross-agent-memory chat-and-llm-quickstart voice-input mcp-tools
    pipeline-chaining prd-dag-orchestrator evals
)

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Curated howto exec_steps audit (${#CURATED[@]} howtos)"

# 1. Every curated howto must exist + have an `exec_steps:` block in its frontmatter.
for h in "${CURATED[@]}"; do
    f="docs/howto/${h}.md"
    if [[ ! -f "$f" ]]; then
        report "$f missing (curated set)"
        continue
    fi
    if ! awk '/^---$/{n++; next} n==1 && /^exec_steps:/{found=1; exit} END{exit !found}' "$f"; then
        report "$f has no exec_steps: block in front-matter"
    fi
done

# 2. Every exec_steps tool: must reference a registered MCP tool. Build the
#    registered-tool set from internal/mcp/*.go.
mapfile -t REGISTERED_TOOLS < <(grep -hoE 'NewTool\("[a-z_][a-z0-9_]*"' internal/mcp/*.go | sed -E 's/NewTool\("([^"]+)"/\1/' | sort -u)
declare -A TOOL_SET
for t in "${REGISTERED_TOOLS[@]}"; do TOOL_SET[$t]=1; done

for h in "${CURATED[@]}"; do
    f="docs/howto/${h}.md"
    [[ -f "$f" ]] || continue
    # Extract `tool: <name>` lines from inside the front-matter block.
    mapfile -t USED < <(awk '
        /^---$/ { n++; next }
        n==1 && /^[[:space:]]*-?[[:space:]]*tool:[[:space:]]/ {
            sub(/^[[:space:]]*-?[[:space:]]*tool:[[:space:]]*/, "")
            sub(/[[:space:]].*$/, "")
            gsub(/"/, "")
            print
        }
        n>=2 { exit }
    ' "$f")
    for t in "${USED[@]}"; do
        if [[ -z "${TOOL_SET[$t]:-}" ]]; then
            report "$f references unregistered MCP tool: $t"
        fi
    done
done

if [[ $ERRORS -gt 0 ]]; then
    echo
    echo "✗ FAIL: $ERRORS curated-howto issue(s)."
    echo "  Rule: AGENT.md §Docs-as-MCP Currency — every curated howto's"
    echo "  exec_steps must reference a registered MCP tool."
    exit 1
fi

echo "✓ PASS: ${#CURATED[@]} curated howtos all have exec_steps; every tool: references a registered MCP tool."
exit 0
