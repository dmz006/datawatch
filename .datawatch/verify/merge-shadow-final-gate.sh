#!/bin/bash
# Verification script for the merge-shadow plan fixes. Read-only: it changes no files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
changed_go() { { git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u | grep "\.go$" | while read -r f; do [ -f "$f" ] && echo "$f"; done; }
fmt_check() { g="$(changed_go)"; [ -z "$g" ] && return 0; bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }

ALLOW="internal/mergeshadow/|internal/autonomous/|docs/|scripts/|\.datawatch/|internal/server/web/docs/|internal/server/cost_report_attrib_test\.go"
for f in $({ git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u); do
  case "$f" in AGENT.md|CLAUDE.md|CLAUDE.local.md) continue;; esac
  echo "$f" | grep -Eq "^($ALLOW)" || fail "changed file outside the plan: $f"
done
go build -o /dev/null ./cmd/datawatch/ || fail "build"
go test ./internal/mergeshadow/ ./internal/autonomous/ -count=1 || fail "package tests"
fmt_check
for s in merge-shadow-release-size-plan merge-shadow-changelog-draft-source merge-shadow-plan-tests merge-shadow-comparison-save merge-shadow-compare-hook-tests merge-shadow-docs; do
  bash ".datawatch/verify/$s.sh" >/dev/null 2>&1 || fail "script $s"
done
echo "verification passed"
exit 0
