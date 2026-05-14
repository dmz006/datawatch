#!/usr/bin/env bash
# scripts/check-howto-coverage.sh — enforces the "Howto-Coverage" rule
# (AGENT.md, BL274 closure).
#
# Every howto under docs/howto/ MUST be either:
#   1. In the curated set with hand-authored exec_steps front-matter, OR
#   2. Explicitly LLM-translation-only (commented in the BL274 plan doc).
#
# A howto with neither flag is invisible to docs_apply and an operator
# trap — the docs viewer surfaces it but no MCP-call sequence backs it.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Howtos explicitly intended for LLM-translation only (per BL274 Q11).
LLM_ONLY=(
    channel-state-engine
    README          # not a howto, just an index
    v7-compute-migration  # narrative migration doc (alpha.15) — explains what auto-migration did, no operator action steps
    dashboard       # PWA-only view; navigation not callable from MCP — reference/conceptual doc
)

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

declare -A LLM_SET
for x in "${LLM_ONLY[@]}"; do LLM_SET[$x]=1; done

echo "==> Howto coverage audit (every docs/howto/*.md must be authored or LLM-only)"

while IFS= read -r f; do
    base="$(basename "$f" .md)"
    if [[ -n "${LLM_SET[$base]:-}" ]]; then
        continue
    fi
    if ! awk '/^---$/{n++; next} n==1 && /^exec_steps:/{found=1; exit} END{exit !found}' "$f"; then
        report "$f has no exec_steps: front-matter and is not in the LLM-only list"
    fi
done < <(find docs/howto -maxdepth 1 -name '*.md' | sort)

if [[ $ERRORS -gt 0 ]]; then
    echo
    echo "✗ FAIL: $ERRORS howto(s) lack coverage."
    echo "  Rule: AGENT.md §Howto-Coverage — every docs/howto/*.md must"
    echo "  carry exec_steps OR be explicitly LLM-only."
    echo "  Add to scripts/check-howto-coverage.sh LLM_ONLY=() if intentional."
    exit 1
fi

echo "✓ PASS: every howto under docs/howto/ has exec_steps or is LLM-only."
exit 0
