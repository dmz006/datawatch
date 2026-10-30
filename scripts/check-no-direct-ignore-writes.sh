#!/usr/bin/env bash
# scripts/check-no-direct-ignore-writes.sh — fails when the generated ignore
# file (.trivyignore) or the generated container table in
# docs/security-review.md differs from what scripts/gen_trivyignore.py
# produces from security/accepted-risks.yml, i.e. when either file was edited
# by hand instead of through the registry.
#
# The working tree is never touched: the registry, the generator and the two
# generated files are copied into a mktemp -d directory with the same relative
# layout, the generator runs there, and its two outputs are diffed against the
# repository copies.
#
# Optional argument: a base ref. When given, a change to .trivyignore in the
# range from that ref to HEAD without a change to security/accepted-risks.yml
# in the same range also fails.
#
# The check runs no tmux command.
# Run independently: bash scripts/check-no-direct-ignore-writes.sh [base-ref]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE_REF="${1:-}"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Generated ignore file audit (.trivyignore, docs/security-review.md)"

INPUTS=(
    security/accepted-risks.yml
    scripts/gen_trivyignore.py
    .trivyignore
    docs/security-review.md
)

for f in "${INPUTS[@]}"; do
    if [[ ! -f "$f" ]]; then
        report "$f is missing"
    fi
done
if [[ $ERRORS -gt 0 ]]; then
    echo "FAIL: $ERRORS problem(s) found"
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for f in "${INPUTS[@]}"; do
    mkdir -p "$TMP/$(dirname "$f")"
    cp "$f" "$TMP/$f"
done

if ! (cd "$TMP" && python3 scripts/gen_trivyignore.py >"$TMP/gen.out" 2>&1); then
    report "scripts/gen_trivyignore.py failed to run against security/accepted-risks.yml:"
    sed 's/^/      /' "$TMP/gen.out"
else
    for f in .trivyignore docs/security-review.md; do
        if ! diff -q "$TMP/$f" "$f" >/dev/null; then
            report "$f differs from the generator output for security/accepted-risks.yml; edit the registry and run: python3 scripts/gen_trivyignore.py"
        fi
    done
fi

if [[ -n "$BASE_REF" ]]; then
    if ! git rev-parse --verify --quiet "$BASE_REF^{commit}" >/dev/null; then
        report "base ref '$BASE_REF' does not resolve to a commit"
    else
        changed="$(git diff --name-only "$BASE_REF..HEAD" -- .trivyignore security/accepted-risks.yml)"
        if grep -qx '.trivyignore' <<<"$changed" && ! grep -qx 'security/accepted-risks.yml' <<<"$changed"; then
            report ".trivyignore changed from $BASE_REF to HEAD without a change to security/accepted-risks.yml"
        fi
    fi
fi

if [[ $ERRORS -gt 0 ]]; then
    echo "FAIL: $ERRORS problem(s) found"
    exit 1
fi
echo "OK: generated ignore files match the registry"
