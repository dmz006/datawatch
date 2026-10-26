#!/usr/bin/env bash
# scripts/check-version-bump.sh — enforces AGENT.md's Versioning rule:
# cmd/datawatch/main.go and internal/server/api.go must carry the same
# `var Version`, and a change that affects behavior must come with a
# version bump.
#
# Found by a 2026-10-07 compliance audit: commit 71397878 made
# substantive changes (new metrics, a new PWA card, a new locale key, a
# gosec suppression) but reused the version label its own parent commit
# had already used. This check exists so that stops happening silently.
#
# Modes (selected by the VERSION_CHECK_MODE env var):
#
#   range  (DEFAULT — VERSION_CHECK_MODE empty or literally "range" also
#           selects it)
#           Compares HEAD against a base that spans the WHOLE pushed
#           range, not just HEAD's parent. This matters because a PRD
#           ships as several automated task commits whose final release
#           commit does the bump; the head commit alone may not have
#           bumped even though the range covers the bump.
#           Base resolution order (first that yields a rev-parse-able ref):
#             1. $1, if non-empty, not the 40-zero sha, and rev-parse-able
#                (this keeps $SMOKE_DIR/check-version-bump.sh "$LAST_TAG"
#                in release-smoke.sh working unchanged)
#             2. $CHECK_VERSION_BASE, same validity rules
#             3. $GITHUB_EVENT_BEFORE, same rules (all-zeros = first push
#                of a branch on GitHub, i.e. "unknown" — move on)
#             4. GITHUB_EVENT_NAME=pull_request with GITHUB_BASE_REF set:
#                merge-base of HEAD with origin/$GITHUB_BASE_REF, falling
#                back to $GITHUB_BASE_REF if the remote ref is absent
#             5. the default branch: $git symbolic-ref refs/remotes/origin/HEAD
#                (with refs/remotes/ stripped), else origin/main, else
#                local main; take merge-base of HEAD with that branch
#             6. nothing resolves: print a "base unknown/skipping"
#                message and exit 0 (preserves the unresolvable-base skip)
#           Pass rule: if ANY non-exempt file changed in
#           `git diff --name-only BASE..HEAD`, the version at HEAD must be
#           NUMERICALLY GREATER (major.minor.patch) than the version at
#           BASE — merely different is not enough. Equal OR lower with
#           non-exempt changes fails.
#
#   commit (legacy, local use)
#           Exactly the old semantics: base = $1 if given, else HEAD^.
#           Equal version + non-exempt change fails; ANY version
#           difference passes.
#
# Exempt (recognized zero-behavior-change) paths, regex:
#   _test\.go$|_test\.js$|^docs/plans/|\.github/workflows/|^\.golangci|^\.gosec-exclude$|^scripts/supervisor-watch\.py$
# A range where every changed file matches this passes without a bump.
#
# Exit codes: 0 = pass or skip, 1 = fail.
#
# The script is relocatable: it cds to its own repo root
# ($(dirname "$0")/..) and makes no other repo-specific assumptions, so
# it can be copied as scripts/check-version-bump.sh into a scratch git
# repo (the test harness relies on this).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"
EXEMPT_RE='_test\.go$|_test\.js$|^docs/plans/|\.github/workflows/|^\.golangci|^\.gosec-exclude$|^scripts/supervisor-watch\.py$'
ZERO_SHA="0000000000000000000000000000000000000000"

# --- mode selection -------------------------------------------------------
MODE="${VERSION_CHECK_MODE:-}"
if [[ -z "$MODE" || "$MODE" == "range" ]]; then
    MODE="range"
fi
if [[ "$MODE" != "commit" && "$MODE" != "range" ]]; then
    echo "✗ FAIL: unrecognized VERSION_CHECK_MODE '${VERSION_CHECK_MODE:-?}' (expected 'commit' or 'range')"
    exit 1
fi

# --- helpers ---------------------------------------------------------------
extract_version() {
    # $1 = git ref ("" means working tree), $2 = file path
    local ref="$1" file="$2" content
    if [[ -z "$ref" ]]; then
        content=$(cat "$file" 2>/dev/null || true)
    else
        content=$(git show "${ref}:${file}" 2>/dev/null || true)
    fi
    printf '%s\n' "$content" | grep -E '^var Version = "' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || true
}

valid_ref() {
    # $1 = candidate ref; true iff non-empty, not the 40-zero sha, and
    # git can resolve it to a commit.
    local ref="${1:-}"
    [[ -n "$ref" ]] || return 1
    [[ "$ref" != "$ZERO_SHA" ]] || return 1
    git rev-parse --quiet --verify "${ref}^{commit}" >/dev/null 2>&1
}

ver_gt() {
    # $1 = a, $2 = b (dotted versions, e.g. 1.2.0); returns 0 iff a > b
    # numerically (major.minor.patch), 1 otherwise (equal or lower).
    awk -v a="$1" -v b="$2" 'BEGIN {
        na = split(a, x, "."); nb = split(b, y, ".")
        n = (na > nb) ? na : nb
        for (i = 1; i <= n; i++) {
            xi = (i <= na && x[i] ~ /^[0-9]+$/) ? x[i] + 0 : 0
            yi = (i <= nb && y[i] ~ /^[0-9]+$/) ? y[i] + 0 : 0
            if (xi > yi) exit 0
            if (xi < yi) exit 1
        }
        exit 1
    }'
}

# --- invariant in BOTH modes: the two files match at head ------------------
CUR_MAIN=$(extract_version "" "$MAIN_FILE")
CUR_API=$(extract_version "" "$API_FILE")

if [[ -z "$CUR_MAIN" || -z "$CUR_API" ]]; then
    echo "✗ FAIL: could not find 'var Version = \"...\"' in $MAIN_FILE and/or $API_FILE"
    exit 1
fi

if [[ "$CUR_MAIN" != "$CUR_API" ]]; then
    echo "✗ FAIL: version mismatch — $MAIN_FILE has $CUR_MAIN, $API_FILE has $CUR_API"
    echo "  AGENT.md Versioning rule: both files MUST match on every commit."
    exit 1
fi

echo "==> Version check: $CUR_MAIN (both files match) [VERSION_CHECK_MODE=$MODE]"

# --- commit mode (legacy): base = $1 or HEAD^; any difference passes ------
if [[ "$MODE" == "commit" ]]; then
    BASE_REF="${1:-HEAD^}"
    if ! valid_ref "$BASE_REF"; then
        echo "  (base ref '$BASE_REF' not resolvable — shallow history, skipping reuse check)"
        exit 0
    fi

    BASE_MAIN=$(extract_version "$BASE_REF" "$MAIN_FILE")

    if [[ -z "$BASE_MAIN" ]]; then
        echo "  (no prior version found at $BASE_REF — nothing to compare)"
        exit 0
    fi

    if [[ "$CUR_MAIN" != "$BASE_MAIN" ]]; then
        echo "✓ PASS: version bumped ($BASE_MAIN -> $CUR_MAIN)"
        exit 0
    fi

    # Version unchanged from base. That's fine ONLY if every changed file is
    # a recognized zero-behavior-change path (tests, plan docs, chore/CI) —
    # the one exception AGENT.md's Versioning rule carves out.
    CHANGED=$(git diff --name-only "$BASE_REF" HEAD 2>/dev/null || true)
    if [[ -z "$CHANGED" ]]; then
        # Nothing changed between base and HEAD (e.g. comparing the working
        # tree against itself) — not a reuse, just unchanged.
        exit 0
    fi

    NON_EXEMPT=$(printf '%s\n' "$CHANGED" | grep -vE "$EXEMPT_RE" || true)
    if [[ -z "$NON_EXEMPT" ]]; then
        echo "✓ PASS: version unchanged ($CUR_MAIN), but every changed file is test-only/plan-doc/chore (recognized exception) [VERSION_CHECK_MODE=$MODE]"
        exit 0
    fi

    echo "✗ FAIL: version $CUR_MAIN reused from base $BASE_REF — these files changed without a version bump:"
    printf '%s\n' "$NON_EXEMPT" | sed 's/^/  - /'
    echo "  AGENT.md Versioning rule: multi-commit features bump on every commit; only"
    echo "  test-only/plan-doc/chore-only commits may skip it."
    exit 1
fi

# --- range mode (default): whole base..HEAD range, numerically greater -----
BASE_REF=""
BASE_HOW=""

if valid_ref "${1:-}"; then
    BASE_REF="$1"; BASE_HOW="\$1"
elif valid_ref "${CHECK_VERSION_BASE:-}"; then
    BASE_REF="$CHECK_VERSION_BASE"; BASE_HOW="\$CHECK_VERSION_BASE"
elif valid_ref "${GITHUB_EVENT_BEFORE:-}"; then
    BASE_REF="$GITHUB_EVENT_BEFORE"; BASE_HOW="\$GITHUB_EVENT_BEFORE"
elif [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" && -n "${GITHUB_BASE_REF:-}" ]]; then
    if BASE_REF=$(git merge-base HEAD "origin/${GITHUB_BASE_REF}" 2>/dev/null); then
        BASE_HOW="merge-base of origin/${GITHUB_BASE_REF}"
    elif BASE_REF=$(git merge-base HEAD "${GITHUB_BASE_REF}" 2>/dev/null); then
        BASE_HOW="merge-base of ${GITHUB_BASE_REF}"
    else
        BASE_REF=""
    fi
fi

# (e) default branch fallback: origin/HEAD, else origin/main, else main.
if [[ -z "$BASE_REF" ]]; then
    DEF_BRANCH=""
    if OGH=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null) && [[ -n "$OGH" ]]; then
        DEF_BRANCH="${OGH#refs/remotes/}"
    fi
    if [[ -z "$DEF_BRANCH" ]]; then
        for candidate in origin/main main; do
            if valid_ref "$candidate"; then DEF_BRANCH="$candidate"; break; fi
        done
    fi
    if [[ -n "$DEF_BRANCH" ]] && BASE_REF=$(git merge-base HEAD "$DEF_BRANCH" 2>/dev/null); then
        BASE_HOW="merge-base of $DEF_BRANCH"
    else
        BASE_REF=""
    fi
fi

# (f) nothing resolved: skip (same behavior as today's unresolvable-base skip).
if [[ -z "$BASE_REF" ]]; then
    echo "  (range mode: base unknown — no resolvable base against \$1/CHECK_VERSION_BASE/GITHUB_EVENT_BEFORE/PR refs/default branch; skipping)"
    exit 0
fi

BASE_MAIN=$(extract_version "$BASE_REF" "$MAIN_FILE")
BASE_API=$(extract_version "$BASE_REF" "$API_FILE")
if [[ -z "$BASE_MAIN" ]]; then
    echo "  (no prior version found at base $BASE_REF — nothing to compare)"
    exit 0
fi
if [[ -n "$BASE_API" && "$BASE_MAIN" != "$BASE_API" ]]; then
    echo "  (note: base $BASE_REF had $BASE_MAIN in $MAIN_FILE but $BASE_API in $API_FILE; comparing against $MAIN_FILE)"
fi

echo "    base: $BASE_REF (resolved via $BASE_HOW), version at base: $BASE_MAIN"

# Non-exempt changes anywhere in base..HEAD require a strictly greater
# version at HEAD; an all-exempt range passes without a bump.
CHANGED=$(git diff --name-only "$BASE_REF" HEAD 2>/dev/null || true)
if [[ -z "$CHANGED" ]]; then
    echo "✓ PASS: no changes between base $BASE_REF and HEAD (VERSION_CHECK_MODE=$MODE)"
    exit 0
fi

NON_EXEMPT=$(printf '%s\n' "$CHANGED" | grep -vE "$EXEMPT_RE" || true)
if [[ -z "$NON_EXEMPT" ]]; then
    echo "✓ PASS: version unchanged ($CUR_MAIN, base $BASE_MAIN at $BASE_REF), but every changed file is test-only/plan-doc/chore (recognized exception) [VERSION_CHECK_MODE=$MODE]"
    exit 0
fi

if ver_gt "$CUR_MAIN" "$BASE_MAIN"; then
    echo "✓ PASS: range $BASE_MAIN -> $CUR_MAIN (base: $BASE_REF, via $BASE_HOW)"
    exit 0
fi

echo "✗ FAIL: version $CUR_MAIN at HEAD is not numerically greater than base version $BASE_MAIN at $BASE_REF (via $BASE_HOW) — these files changed without a version bump:"
printf '%s\n' "$NON_EXEMPT" | sed 's/^/  - /'
echo "  AGENT.md Versioning rule: multi-commit features bump the version for the PRD range; only"
echo "  test-only/plan-doc/chore-only changes may skip it."
exit 1
