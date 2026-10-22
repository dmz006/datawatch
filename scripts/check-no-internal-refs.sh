#!/bin/sh
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
# POSIX-sh compatible: may be invoked as `sh scripts/check-no-internal-refs.sh`.
# Run as part of release-smoke.sh; failing exit code blocks the release.
# Run independently: sh scripts/check-no-internal-refs.sh

set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ERRORS=0
# report_lines <label-prefixed-lines> : report stored grep output (one match
# per line) without process substitution so the script stays POSIX-safe.
report_lines() {
    _matches="$1"
    _n=$(printf '%s\n' "$_matches" | wc -l | tr -d ' \t')
    ERRORS=$((ERRORS + _n))
    printf '%s\n' "$_matches" | while IFS= read -r line; do
        echo "  ✗ $line"
    done
}

echo "==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)"

# 1. Locale bundles — no BL###/F##/B##/S## allowed in any string value.
for f in internal/server/web/locales/*.json; do
    [ -f "$f" ] || continue
    matches=$(grep -nE '"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' "$f" || true)
    [ -z "$matches" ] || report_lines "$f: $matches"
done

# 2. OpenAPI summaries (Swagger UI is operator-facing).
for f in internal/server/web/openapi.yaml internal/server/web/docs/api/openapi.yaml; do
    [ -f "$f" ] || continue
    matches=$(grep -nE '^\s*summary:.*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)' "$f" || true)
    [ -z "$matches" ] || report_lines "$f summary: $matches"
done

# 3. REST API JSON `"note":` field strings (operator surface).
matches=$(grep -rnE '"note":\s*"[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/*.go || true)
[ -z "$matches" ] || report_lines "API note: $matches"

# 4. PWA tooltip / title attributes and visible fallback strings.
#    Comment lines (//, <!--, *) are exempt.
if [ -f internal/server/web/app.js ]; then
    matches=$(grep -nE 'title="[^"]*\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\b[^"]*"' internal/server/web/app.js | grep -vE '^\s*[0-9]+:\s*//|<!--|^\s*[0-9]+:\s*\*' || true)
    [ -z "$matches" ] || report_lines "PWA title: $matches"
fi

# 5b. README.md — the project's public front door (2026-10-07: an audit
#     found and fixed 2 stray instances here; now enforced going forward).
matches=$(grep -nE '\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' README.md || true)
[ -z "$matches" ] || report_lines "README.md: $matches"

# 5. datawatch-definitions.md must not contain internal refs (operator doc).
if [ -f docs/datawatch-definitions.md ]; then
    matches=$(grep -nE '\b(BL[0-9]+|F[0-9]+\b|B[0-9]+\b|S[0-9]+\b)' docs/datawatch-definitions.md || true)
    [ -z "$matches" ] || report_lines "datawatch-definitions.md: $matches"
fi

# 6. howto/* prose must not contain internal refs (operator-facing tutorials).
#    Frontmatter tool/step names (exec_steps) are exempt as they are
#    machine-readable identifiers, not prose. Comment lines (<!-- -->) are
#    exempt. identity-and-telos.md example YAML uses BL### as placeholder
#    project names in user-editable fields — grep those out via line context.
if ls docs/howto/*.md 2>/dev/null | head -1 > /dev/null 2>&1; then
    matches=$(grep -rnE '\b(BL[0-9]{3,}|F[0-9]{2}\b|B[0-9]{2}\b)\b' docs/howto/*.md \
        | grep -vE '^\S+:\s*[0-9]+:\s*(#|//|<!--|---|\s+tool:|description:|args:)' \
        | grep -vE 'identity-and-telos\.md' \
        || true)
    [ -z "$matches" ] || report_lines "$matches"
fi

if [ "$ERRORS" -gt 0 ]; then
    echo
    echo "✗ FAIL: $ERRORS internal-ref leak(s) in user-facing surface."
    echo "  Rule: feedback_user_facing_docs_no_internals — internal IDs belong"
    echo "  in plans/, CHANGELOG, and commit messages, not in operator-visible UI."
    exit 1
fi

echo "✓ PASS: no internal refs in user-facing surfaces."
exit 0
