#!/usr/bin/env bash
# Verify the path-injection fixes in internal/autonomous/file_drift.go,
# internal/autonomous/uncommitted_work.go, internal/autonomous/cleanup_status.go.
# Checks only the files of this unit; proves the scanner barrier with
# scripts/codeql-local.sh --languages go (documented scanner interface).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

files="internal/autonomous/file_drift.go internal/autonomous/uncommitted_work.go internal/autonomous/cleanup_status.go"

# 1. gofmt on this unit's Go files only (never over a package or directory).
for f in $files; do
  if [ -f "$f" ]; then
    bad="$(gofmt -l "$f" || true)"
    if [ -n "$bad" ]; then echo "FAIL: gofmt -l reports: $bad"; exit 1; fi
  fi
done
echo "OK: gofmt clean on the touched Go files"

# 2. cumulative changed set vs merge base (presence check; artefact dirs and
#    CHANGELOG.md excluded by dw_changed_set, per artefact_files_allowed).
changed="$(dw_changed_set)"
for f in $files; do
  printf '%s\n' "$changed" | grep -qx -- "$f" || echo "NOTE: $f not present in the cumulative changed set yet"
done

# 3. local CodeQL proof.
scan="$(dw_run_scan go)"
rc=0
for f in $files; do
  dw_assert_pair "$scan" "go/path-injection" "$f" || rc=1
done
[ "$rc" -eq 0 ] || exit 1

# 4. build + tests for the touched package stay green.
go build ./internal/autonomous/ || { echo "FAIL: go build ./internal/autonomous/"; exit 1; }
go test ./internal/autonomous/ || { echo "FAIL: go test ./internal/autonomous/"; exit 1; }
echo "OK: go build and go test for ./internal/autonomous/ pass"

echo "PASS: harden-path-access"
