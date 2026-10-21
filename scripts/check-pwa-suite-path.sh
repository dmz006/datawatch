#!/usr/bin/env bash
# scripts/check-pwa-suite-path.sh — verifies the PWA story-test suite
# directory is present before a runner tries to use it.
#
# Usage: check-pwa-suite-path.sh [suite-dir]
#
# Defaults to scripts/test-stories/pwa, resolved relative to this
# script's own location (not the caller's working directory). Passes
# silently (exit 0) when the directory exists, contains package.json,
# and holds at least one TS-*.mjs story. Otherwise writes one line to
# stderr naming the missing condition and exits 1.
set -euo pipefail

SUITE_DIR="${1:-$(dirname "${BASH_SOURCE[0]}")/test-stories/pwa}"

if [[ ! -d "$SUITE_DIR" ]]; then
  echo "PWA suite not found: $SUITE_DIR (directory does not exist)" >&2
  exit 1
fi

if [[ ! -f "$SUITE_DIR/package.json" ]]; then
  echo "PWA suite not found: $SUITE_DIR (missing package.json)" >&2
  exit 1
fi

if ! compgen -G "$SUITE_DIR/TS-*.mjs" >/dev/null; then
  echo "PWA suite not found: $SUITE_DIR (no TS-*.mjs story files)" >&2
  exit 1
fi
