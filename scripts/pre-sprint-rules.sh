#!/usr/bin/env bash
# scripts/pre-sprint-rules.sh — operator-required mechanical trigger for the
# AGENT.md "Pre-Execution Rule" (re-read applicable rules before code changes).
#
# Usage:
#   bash scripts/pre-sprint-rules.sh sprint-start   # full multi-sprint feature
#   bash scripts/pre-sprint-rules.sh patch          # single-issue bug-fix
#   bash scripts/pre-sprint-rules.sh docs-only      # CHANGELOG / howto / plan-doc
#   bash scripts/pre-sprint-rules.sh security       # gosec / dep audit / TLS
#   bash scripts/pre-sprint-rules.sh ui             # PWA / locale / mobile-parity
#
# Prints the rule sections most likely to apply so they're in front of you
# before keyboard touches code.

set -uo pipefail
# Note: not set -e — `grep` returning non-zero on no-match would otherwise
# kill the script mid-loop. We tolerate misses + emit "[not found]" instead.

mode="${1:-sprint-start}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
AGENT="$ROOT/AGENT.md"

declare -A SECTIONS_FOR_MODE=(
    [sprint-start]="Pre-Execution Code-Quality Testing-Tracker Versioning Planning Documentation Configuration-Accessibility Localization Mobile-Parity Live-Project-Cookbook Functional-Change-Checklist Memory-Use Release-workflow Binary-build-cadence README-current Mobile-Parity"
    [patch]="Pre-Execution Code-Quality Versioning Documentation Mobile-Parity Background-Shell-Cleanup Release-vs-Patch"
    [docs-only]="Documentation No-internal-IDs README-current Backlog-refactor Embedded-docs-current"
    [security]="Pre-release-dependency-audit Pre-release-security-scan Security-Rules Secrets-Store-Rule No-local-environment-leaks"
    [ui]="Configuration-Accessibility Localization Mobile-Parity Live-Project-Cookbook"
)

if [[ -z "${SECTIONS_FOR_MODE[$mode]:-}" ]]; then
    echo "unknown mode: $mode"
    echo "valid: sprint-start | patch | docs-only | security | ui"
    exit 1
fi

echo "==> Pre-Execution rule reminder for: $mode"
echo
echo "Re-read these AGENT.md sections before keyboard touches code:"
echo

for section in ${SECTIONS_FOR_MODE[$mode]}; do
    pretty=$(echo "$section" | tr '-' ' ')
    # Try the hyphenated form first (matches AGENT.md sections like "## Pre-Execution Rule"),
    # then fall back to the spaced form for sections written without a hyphen.
    line=$(grep -nE "^## " "$AGENT" | grep -i "$section" | head -1 | cut -d: -f1)
    if [[ -z "$line" ]]; then
        line=$(grep -nE "^## " "$AGENT" | grep -i "$pretty" | head -1 | cut -d: -f1)
    fi
    if [[ -n "$line" ]]; then
        title=$(sed -n "${line}p" "$AGENT" | sed 's/^## //')
        echo "  • L$line — $title"
        echo "      $(sed -n "$((line+2))p" "$AGENT" | head -c 160)"
    else
        echo "  • [not found in AGENT.md] — $section"
    fi
    echo
done

echo "==> Cheap pre-flight checks (run these now):"
echo "  • $(./scripts/check-no-internal-refs.sh 2>&1 | tail -1)"
echo "  • $(./scripts/tidy-plans.sh --check 2>&1 | tail -1)"
if [[ "$mode" == "ui" ]] || [[ "$mode" == "sprint-start" ]]; then
    echo "  • node --check internal/server/web/app.js && echo 'PWA JS syntax: ok'"
fi
echo
echo "==> Live cookbook (current task list):"
echo "    Use TaskList tool in Claude Code session."
