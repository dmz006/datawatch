#!/usr/bin/env bash
# Lint as CI runs it: the linter version CI pins, built with the Go version of
# go.mod, after the generated docs exist, with the private leak list required.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_var MERGE_PROJECT_DIR
merge_need_tool go

version=$(grep -A3 golangci-lint-action .github/workflows/ci.yaml | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
[[ -n "$version" ]] || merge_die "no pinned linter version in .github/workflows/ci.yaml"
gotool=go$(awk '/^go /{print $2; exit}' go.mod)
tools="${MERGE_TOOL_DIR:-$(go env GOPATH)/merge-tools}/golangci-lint-$version"
if ! "$tools/golangci-lint" version 2>/dev/null | grep -q "version ${version#v} "; then
    GOTOOLCHAIN="$gotool" GOBIN="$tools" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$version"
fi

# The private list is never committed, so it is read from the project's own
# checkout. A missing or empty list fails the check.
export LEAK_CHECK_LOCAL="$MERGE_PROJECT_DIR/.leak-patterns.local"
export LEAK_CHECK_REQUIRE_LOCAL=1

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# A lint cache of this run's own. Every merge works in a new scratch folder,
# and a cache shared with other checkouts replays their findings under their
# paths, where this checkout's exclusions do not apply. MERGE_LINT_CACHE names
# another one for a run by hand.
export GOLANGCI_LINT_CACHE="${MERGE_LINT_CACHE:-$work/lint-cache}"

make sync-docs docs-index
# The linter takes a lock for the whole machine: wait for another run.
log="$work/lint.log"
for _ in $(seq 1 60); do
    if PATH="$tools:$PATH" GOTOOLCHAIN="$gotool" nice -n 10 make lint >"$log" 2>&1; then
        cat "$log"
        exit 0
    fi
    grep -q 'parallel golangci-lint is running' "$log" || break
    sleep 20
done
cat "$log"
exit 1
