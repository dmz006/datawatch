#!/usr/bin/env bash
# scripts/check-gofmt.sh — report Go files under cmd/ and internal/ that are not
# gofmt-formatted.
#
# Listing comes from `gofmt -l cmd internal`. Excluded from the report:
#   - files with a "Code generated" header (generator output is never re-formatted by hand)
#   - files under any testdata/ or vendor/ path
#
# Any remaining offender is listed with its fix command (`gofmt -w <file>`) and
# the script exits 1; a clean tree prints OK and exits 0. The script only
# reads files and runs `gofmt -l` — it never writes or formats anything.
# Run independently: bash scripts/check-gofmt.sh

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v gofmt >/dev/null 2>&1; then
    echo "FAIL: gofmt not found (install the Go toolchain)" >&2
    exit 1
fi

offenders=()
while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    case "/$f" in
        */testdata/*|*/vendor/*) continue ;;
    esac
    header="$(head -n 5 "$f" 2>/dev/null || true)"
    case "$header" in
        *"Code generated"*) continue ;;
    esac
    offenders+=("$f")
done < <(gofmt -l cmd internal)

if ((${#offenders[@]} > 0)); then
    echo "Unformatted Go files (excluded: 'Code generated' headers, testdata/, vendor/):"
    for f in "${offenders[@]}"; do
        echo "  ✗ $f    (fix: gofmt -w $f)"
    done
    echo "FAIL: ${#offenders[@]} unformatted file(s)"
    exit 1
fi

echo "OK: all Go files under cmd/ and internal/ are gofmt-formatted"
