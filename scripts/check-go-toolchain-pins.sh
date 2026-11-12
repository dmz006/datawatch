#!/usr/bin/env bash
# scripts/check-go-toolchain-pins.sh — enforces a Go toolchain floor across
# every place the repo pins a Go version:
#   1. docker/dockerfiles/*: `ARG GO_VERSION=` / `ARG GH_GO_VERSION=` and any
#      literal `golang:<ver>` in a FROM line
#   2. .github/workflows/*.y*ml: `go-version:` values
#      (`go-version-file: go.mod` is fine — it follows go.mod)
#   3. go.mod's `go` directive
#
# Every explicit pin must be an exact X.Y.Z >= FLOOR. Floating pins
# (`1.26`, `1.27.x`, `1.x`, `stable`) can resolve below the floor, so they
# fail. Go 1.27.0-1.27.2 are also rejected (CVE-2026-111943: fixed in 1.27.3).
#
# Usage: scripts/check-go-toolchain-pins.sh [--self-test]

set -euo pipefail

FLOOR="1.26.10"

# version_lt a b — true if a < b (both "X.Y.Z").
version_lt() {
    [[ "$1" == "$2" ]] && return 1
    local lower
    lower=$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)
    [[ "$lower" == "$1" ]]
}

# check_pin where value — prints a failure and returns 1 if value is unacceptable.
check_pin() {
    local where="$1" value="$2"
    if [[ ! "$value" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "✗ FAIL: $where pins '$value' — must be an exact X.Y.Z >= $FLOOR (floating pins can resolve below the floor)" >&2
        return 1
    fi
    if version_lt "$value" "$FLOOR"; then
        echo "✗ FAIL: $where pins $value, below the floor $FLOOR" >&2
        return 1
    fi
    if [[ "$value" =~ ^1\.27\.([0-9]+)$ ]] && (( BASH_REMATCH[1] < 3 )); then
        echo "✗ FAIL: $where pins $value, vulnerable to CVE-2026-111943 (need >= 1.27.3)" >&2
        return 1
    fi
    return 0
}

# run_checks root — runs all three checks against the tree under root.
run_checks() {
    local root="$1" fail=0 file value
    shopt -s nullglob

    for file in "$root"/docker/dockerfiles/*; do
        [[ -f "$file" ]] || continue
        while IFS= read -r value; do
            check_pin "$file (ARG)" "$value" || fail=1
        done < <(grep -oE '^ARG (GH_)?GO_VERSION=[^[:space:]]+' "$file" | sed -E 's/^ARG (GH_)?GO_VERSION=//' | tr -d "\"'" || true)
        while IFS= read -r value; do
            [[ "$value" == '$'* ]] && continue  # resolved via an ARG, checked above
            check_pin "$file (FROM golang:)" "$value" || fail=1
        done < <(grep -E '^FROM ' "$file" | grep -oE 'golang:[^[:space:]-]+' | sed 's/^golang://' || true)
    done

    for file in "$root"/.github/workflows/*.yml "$root"/.github/workflows/*.yaml; do
        while IFS= read -r value; do
            check_pin "$file (go-version)" "$value" || fail=1
        done < <(grep -E '^[[:space:]-]*go-version:' "$file" | sed -E 's/^[[:space:]-]*go-version:[[:space:]]*//; s/[[:space:]]+#.*$//' | tr -d "\"'" || true)
    done

    value=$(grep -E '^go [^[:space:]]+' "$root/go.mod" 2>/dev/null | head -1 | awk '{print $2}' || true)
    if [[ -z "$value" ]]; then
        echo "✗ FAIL: could not find a 'go' directive in $root/go.mod" >&2
        fail=1
    else
        check_pin "go.mod" "$value" || fail=1
    fi

    shopt -u nullglob
    return $fail
}

self_test() {
    local tmp rc=0
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' RETURN

    # fixture dockerfile_pin workflow_line — builds a fresh tree and returns
    # run_checks' status.
    fixture() {
        local dir
        dir=$(mktemp -d -p "$tmp")
        mkdir -p "$dir/docker/dockerfiles" "$dir/.github/workflows"
        printf 'module x\n\ngo %s\n' "$FLOOR" > "$dir/go.mod"
        printf 'ARG GO_VERSION=%s\nFROM golang:${GO_VERSION}-bookworm\n' "$1" > "$dir/docker/dockerfiles/Dockerfile.t"
        printf 'jobs:\n  t:\n    steps:\n      - uses: actions/setup-go@v5\n        with:\n          %s\n' "$2" > "$dir/.github/workflows/t.yaml"
        run_checks "$dir" 2>/dev/null
    }

    expect() {
        local want="$1" name="$2" got=0
        shift 2
        fixture "$@" || got=$?
        [[ $got -ne 0 ]] && got=1
        if [[ "$got" != "$want" ]]; then
            echo "✗ self-test FAIL: $name (expected exit $want, got $got)" >&2
            rc=1
        else
            echo "  ok: $name"
        fi
    }

    expect 1 "Dockerfile pin 1.26.9 fails"        1.26.9  'go-version-file: go.mod'
    expect 1 "Dockerfile pin 1.27.2 fails"        1.27.2  'go-version-file: go.mod'
    expect 1 "Dockerfile floating 1.26 fails"     1.26    'go-version-file: go.mod'
    expect 1 "workflow go-version: stable fails"  1.26.10 'go-version: stable'
    expect 1 "workflow go-version: 1.27.x fails"  1.26.10 'go-version: 1.27.x'
    expect 0 "Dockerfile pin 1.26.10 passes"      1.26.10 'go-version-file: go.mod'
    expect 0 "Dockerfile pin 1.27.3 passes"       1.27.3  'go-version-file: go.mod'
    expect 0 "workflow go-version-file passes"    1.26.10 'go-version-file: go.mod'
    expect 0 "workflow go-version: 1.27.3 passes" 1.26.10 'go-version: "1.27.3"'

    if [[ $rc -ne 0 ]]; then
        exit 1
    fi
    echo "✓ PASS: self-test"
}

if [[ "${1:-}" == "--self-test" ]]; then
    self_test
    exit 0
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if run_checks "$ROOT"; then
    echo "✓ PASS: all Go toolchain pins are exact and >= $FLOOR"
else
    echo "  Fix: pin an exact X.Y.Z >= $FLOOR (and not 1.27.0-1.27.2), or use go-version-file: go.mod." >&2
    exit 1
fi
