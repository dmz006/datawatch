#!/usr/bin/env bash
# scripts/check-no-internal-refs.sh — enforces the
# "User-facing docs strip internal refs" rule
# (memory: feedback_user_facing_docs_no_internals).
#
# Internal refs (BL###, F##, B##, S##) belong in plans/, CHANGELOG, and
# commit messages. They MUST NOT appear in:
#   - locale bundles (operator-rendered strings)
#   - openapi.yaml summaries (Swagger UI)
#   - REST API JSON response notes / fields
#   - PWA tooltips / button titles / fallback strings
#   - howto/* prose (operator-facing tutorials)
#   - datawatch-definitions.md
#
# Run as part of release-smoke.sh; failing exit code blocks the release.
# Run independently: bash scripts/check-no-internal-refs.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

echo "==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)"

# 1. Locale bundles — no BL###/F##/B##/S## allowed in any string value.
for f in internal/server/web/locales/*.json; do
    if grep -qE '"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' "$f"; then
        while IFS= read -r line; do
            report "$f: $line"
        done < <(grep -nE '"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' "$f")
    fi
done

# 2. OpenAPI summaries (Swagger UI is operator-facing).
for f in internal/server/web/openapi.yaml internal/server/web/docs/api/openapi.yaml; do
    [[ -f "$f" ]] || continue
    if grep -qE '^\s*summary:.*\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' "$f"; then
        while IFS= read -r line; do
            report "$f summary: $line"
        done < <(grep -nE '^\s*summary:.*\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' "$f")
    fi
done

# 3. REST API JSON `"note":` field strings (operator surface).
if grep -rqnE '"note":\s*"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/*.go; then
    while IFS= read -r line; do
        report "API note: $line"
    done < <(grep -rnE '"note":\s*"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/*.go)
fi

# 4. PWA tooltip / title attributes and visible fallback strings.
#    Comment lines (//, <!--, *) are exempt.
if grep -nE 'title="[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/web/app.js | grep -vE '^\s*[0-9]+:\s*//|<!--|^\s*[0-9]+:\s*\*' > /dev/null; then
    while IFS= read -r line; do
        report "PWA title: $line"
    done < <(grep -nE 'title="[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/web/app.js | grep -vE ':\s*//|<!--')
fi

# 5. datawatch-definitions.md must not contain internal refs (operator doc).
if [[ -f docs/datawatch-definitions.md ]]; then
    if grep -qE '\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' docs/datawatch-definitions.md; then
        while IFS= read -r line; do
            report "datawatch-definitions.md: $line"
        done < <(grep -nE '\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' docs/datawatch-definitions.md)
    fi
fi

if [[ $ERRORS -gt 0 ]]; then
    echo
    echo "✗ FAIL: $ERRORS internal-ref leak(s) in user-facing surface."
    echo "  Rule: feedback_user_facing_docs_no_internals — internal IDs belong"
    echo "  in plans/, CHANGELOG, and commit messages, not in operator-visible UI."
    exit 1
fi

echo "✓ PASS: no internal refs in user-facing surfaces."
exit 0
