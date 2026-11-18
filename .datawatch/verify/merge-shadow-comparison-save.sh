#!/bin/bash
# Verification script for the merge-shadow plan fixes. Read-only: it changes no files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
changed_go() { { git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u | grep "\.go$" | while read -r f; do [ -f "$f" ] && echo "$f"; done; }
fmt_check() { g="$(changed_go)"; [ -z "$g" ] && return 0; bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }

go build ./internal/autonomous/ || fail "build"
grep -n "Save" internal/autonomous/merge_shadow_compare_hook.go | grep -qi "comparison\|store" || fail "the compare hook does not call the comparisons store save"
fmt_check
echo "verification passed"
exit 0
