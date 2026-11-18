#!/bin/bash
# Verification script for the merge-shadow plan fixes. Read-only: it changes no files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
changed_go() { { git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u | grep "\.go$" | while read -r f; do [ -f "$f" ] && echo "$f"; done; }
fmt_check() { g="$(changed_go)"; [ -z "$g" ] && return 0; bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }

grep -qi "comparisons store" docs/howto/merge-shadow.md || fail "how-to does not describe the comparisons store"
grep -qi "comparisons store" docs/operations.md || fail "operations manual does not describe the comparisons store"
! grep -qi "has not landed" docs/operations.md || fail "operations manual still says the wiring has not landed"
echo "verification passed"
exit 0
