#!/usr/bin/env bash
# Verify the command-injection fix in internal/autonomous/file_drift.go.
# Checks only the files of this unit; proves the scanner barrier with
# scripts/codeql-local.sh --languages go (documented scanner interface).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

target="internal/autonomous/file_drift.go"

# 1. gofmt on this unit's Go file only (never over a package or directory).
if [ -f "$target" ]; then
  bad="$(gofmt -l "$target" || true)"
  if [ -n "$bad" ]; then echo "FAIL: gofmt -l reports: $bad"; exit 1; fi
fi
echo "OK: gofmt clean on $target"

# 2. cumulative changed set vs merge base (presence check; artefact dirs and
#    CHANGELOG.md excluded by dw_changed_set, per artefact_files_allowed).
changed="$(dw_changed_set)"
printf '%s\n' "$changed" | grep -qx -- "$target" || echo "NOTE: $target not present in the cumulative changed set yet"

# 3. local CodeQL proof.
scan="$(dw_run_scan go)"
dw_assert_pair "$scan" "go/command-injection" "$target" || exit 1

# 4. build + tests for the touched package stay green.
go build ./internal/autonomous/ || { echo "FAIL: go build ./internal/autonomous/"; exit 1; }
go test ./internal/autonomous/ || { echo "FAIL: go test ./internal/autonomous/"; exit 1; }
echo "OK: go build and go test for ./internal/autonomous/ pass"

echo "PASS: harden-git-runner"
