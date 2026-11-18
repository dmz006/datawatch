#!/bin/bash
# Verification script for the CodeQL round three hardening. Read-only: it changes no files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
CHANGED="$({ git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u)"
need() { echo "$CHANGED" | grep -qx "$1" || fail "$1 is not in the changed file set"; }
fmt_go() { g="$(echo "$CHANGED" | grep "\.go$" | while read -r f; do [ -f "$f" ] && echo "$f"; done)"; [ -z "$g" ] && return 0; bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }
nofix() { echo "$CHANGED" | grep -E "^(AGENT|CLAUDE)\.md$" >/dev/null && fail "launcher file changed"; return 0; }

need internal/autonomous/file_drift.go
go build ./internal/autonomous/ || fail "build"
go vet ./internal/autonomous/ || fail "vet"
env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ -run "Drift" -count=1 || fail "drift tests"
fmt_go
nofix
echo "verification passed"
exit 0
