#!/bin/bash
# Read-only final gate for the burn-guard pace-line work: build, touched-package tests, formatting of changed Go files.
set -u
cd "$(git rev-parse --show-toplevel)"
fail() { echo "FAIL: $*"; exit 1; }
MB="$(git merge-base HEAD main)"
for f in $({ git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u); do
  case "$f" in
    AGENT.md|CLAUDE.md|CLAUDE.local.md|.datawatch/*|docs/plans/*|internal/server/web/docs/*) continue;;
    cmd/datawatch/*|internal/config/*|internal/mcp/*|internal/router/*|internal/server/*|internal/autonomous/*|internal/usage/*|docs/*|CHANGELOG.md|scripts/*) ;;
    *) fail "changed file outside the plan: $f";;
  esac
done
go build -o /dev/null ./cmd/datawatch/ || fail "build"
go vet ./cmd/datawatch/ ./internal/config/ ./internal/mcp/ ./internal/router/ ./internal/server/ || fail "vet"
go test ./internal/config/ ./internal/mcp/ ./internal/router/ ./internal/server/ ./cmd/datawatch/ -count=1 || fail "package tests"
g="$({ git diff --name-only "$MB"; git ls-files --others --exclude-standard; } | sort -u | grep '\.go$' | while read -r f; do [ -f "$f" ] && echo "$f"; done)"
[ -z "$g" ] || { bad="$(echo "$g" | xargs gofmt -l)"; [ -z "$bad" ] || fail "gofmt needed: $bad"; }
(cd internal/server/web && node --test app-burn-guard.test.js >/dev/null 2>&1) || fail "burn guard PWA test"
echo "verification passed"
