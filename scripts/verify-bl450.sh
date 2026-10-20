#!/usr/bin/env bash
# Verifies the startup-dialog auto-dismiss regression tests and the investigation notes exist.
set -euo pipefail
cd "$(dirname "$0")/.."
test -s docs/plans/bl450-startup-dialog-investigation.md
go vet ./internal/session/
go test ./internal/session/ -run 'StartupDialog' -count=1
