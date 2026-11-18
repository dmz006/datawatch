#!/bin/bash
# Verification script for the CodeQL round three hardening. Read-only: it changes no files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
CHANGED="$({ git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | grep -vE '^(worker-starting-context\.md|AGENT\.md|CLAUDE\.md|docs/plans/(drafts|parity-issues|reports)/)' | sort -u)"
need() { echo "$CHANGED" | grep -qx "$1" || fail "$1 is not in the changed file set"; }
fmt_go() { g="$(echo "$CHANGED" | grep "\.go$" | while read -r f; do [ -f "$f" ] && echo "$f"; done)"; [ -z "$g" ] && return 0; bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }
nofix() { git diff --name-only "$MB" HEAD | grep -E "^(AGENT|CLAUDE)\.md$" >/dev/null && fail "launcher file committed"; return 0; }

ALLOW="internal/autonomous/|internal/server/web/|docs/|scripts/|CHANGELOG.md|\.datawatch/"
for f in $CHANGED; do case "$f" in AGENT.md|CLAUDE.md) continue;; esac; echo "$f" | grep -Eq "^($ALLOW)" || fail "changed file outside the plan: $f"; done
go build -o /dev/null ./cmd/datawatch/ || fail "build"
env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ -count=1 || fail "autonomous tests"
fmt_go
node --check internal/server/web/app.js || fail "app.js syntax"
for s in cq3-go-command cq3-go-paths cq3-web-xss cq3-web-tests; do bash ".datawatch/verify/$s.sh" >/dev/null 2>&1 || fail "script $s"; done
nofix
echo "verification passed"
exit 0
