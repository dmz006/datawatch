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

need internal/server/web/app-task-routing.test.js
need internal/server/web/app-plan-records.test.js
node --check internal/server/web/app-task-routing.test.js || fail "syntax"
node --check internal/server/web/app-plan-records.test.js || fail "syntax"
(cd internal/server/web && node --test app-task-routing.test.js app-plan-records.test.js >/dev/null 2>&1) || fail "tests"
nofix
echo "verification passed"
exit 0
