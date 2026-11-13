#!/usr/bin/env bash
# scripts/check-no-load-patterns.sh — fails when a test file or test helper
# holds a CPU-burner or stress pattern. Tests must be deterministic: a failure
# is a defect, never "load", so nothing under test may spin the CPU.
#
# Scanned (relative to the root): Go *_test.go files, every testdata/ folder,
# scripts/tests/, tests/, scripts/*_test.sh, and JS *.test.js files under
# internal/server/web.
#
# Flagged patterns, each reported as file:line:
#   - shell busy loops: `while :; do :; done`, `while true; do :; done`
#   - `yes > /dev/null`
#   - the commands stress, stress-ng and cpuburn
#   - `dd if=/dev/zero of=/dev/null`
#   - Go empty spin loops: `for {}` and `for { }`
#   - runtime.GOMAXPROCS worker fan-outs described as load/burn/stress/spin
#   - the helper names startConcurrentLoad and loadWorkers
#
# Excluded: this script, its own test (scripts/check-no-load-patterns_test.sh)
# and any path listed in scripts/check-no-load-patterns.allow (one relative
# path per line; absent by design — there is no legitimate bounded use today).
#
# Usage: bash scripts/check-no-load-patterns.sh [root]
#   root  optional tree to scan (default: the repository root); the test uses
#         it to point the check at temporary fixture trees.
# Exit codes: 0 clean, 1 hits found, 2 bad root.

set -uo pipefail

SELF_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="${1:-$(cd "$SELF_DIR/.." && pwd)}"
[[ -d "$ROOT" ]] || { echo "check-no-load-patterns: not a directory: $ROOT" >&2; exit 2; }
ROOT="$(cd "$ROOT" && pwd)"

ALLOW="$ROOT/scripts/check-no-load-patterns.allow"

# Candidate files, NUL-separated, relative to ROOT.
list_files() {
    (
        cd "$ROOT" || exit 0
        find . \( -name .git -o -name node_modules \) -prune -o -type f \( \
            -name '*_test.go' \
            -o -path '*/testdata/*' \
            -o -path './scripts/tests/*' \
            -o -path './tests/*' \
            -o -path './scripts/*_test.sh' \
            -o -path './internal/server/web/*' -name '*.test.js' \
            \) -print0
    )
}

# name<TAB>extended regex (grep -E); matched case-sensitively per line.
PATTERNS=(
    $'busy loop\twhile[[:space:]]+(:|true)[[:space:]]*;[[:space:]]*do[[:space:]]+:[[:space:]]*;[[:space:]]*done'
    $'yes to /dev/null\t(^|[^[:alnum:]_])yes[[:space:]]*>[[:space:]]*/dev/null'
    $'burner command\t(^|[^[:alnum:]_./-])(stress-ng|cpuburn)([^[:alnum:]_-]|$)|(^|[;&|(`]|\\$\\()[[:space:]]*((timeout|nice|sudo|env|exec)[[:space:]]+([^;&|]*[[:space:]])?)?stress([[:space:];&|)`]|$)|Command(Context)?\\([^)]*"(stress-ng|stress|cpuburn)"'
    $'dd zero to null\tdd[[:space:]]+if=/dev/zero[[:space:]]+of=/dev/null'
    $'Go empty spin loop\t(^|[^[:alnum:]_])for[[:space:]]*\\{[[:space:]]*\\}'
    $'GOMAXPROCS load fan-out\tGOMAXPROCS.*([Ll]oad|[Bb]urn|[Ss]tress|[Ss]pin)|([Ll]oad|[Bb]urn|[Ss]tress|[Ss]pin).*GOMAXPROCS'
    $'load helper name\t(startConcurrentLoad|loadWorkers)'
)

hits=0
while IFS= read -r -d '' f; do
    rel="${f#./}"
    case "$rel" in
        scripts/check-no-load-patterns.sh | scripts/check-no-load-patterns_test.sh) continue ;;
    esac
    if [[ -f "$ALLOW" ]] && grep -qxF -- "$rel" "$ALLOW"; then continue; fi
    for entry in "${PATTERNS[@]}"; do
        name="${entry%%$'\t'*}"
        re="${entry#*$'\t'}"
        while IFS=: read -r ln text; do
            [[ -n "$ln" ]] || continue
            echo "  ✗ $rel:$ln: $name: ${text## }"
            hits=$((hits + 1))
        done < <(cd "$ROOT" && grep -InE -e "$re" -- "$rel" 2>/dev/null || true)
    done
done < <(list_files)

if [[ $hits -gt 0 ]]; then
    echo "check-no-load-patterns: $hits CPU-burner/stress pattern hit(s). Tests must be deterministic; replace with a fake or a bounded wait." >&2
    exit 1
fi
echo "check-no-load-patterns: OK"

