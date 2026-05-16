#!/usr/bin/env bash
# run-tests.sh — forwarding wrapper.
#
# The test runner lives outside this repo at ../datawatch-testing/run-tests.sh
# so test artifacts (isolated daemon data dir, run evidence) never touch the
# source tree.
#
# Usage: all flags are forwarded verbatim.
#   bash scripts/run-tests.sh [--surface=api] [--feature=sessions] ...
#
# See ../datawatch-testing/README.md for full documentation.

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
EXTERNAL_RUNNER="$SCRIPT_DIR/../../datawatch-testing/run-tests.sh"

if [[ ! -f "$EXTERNAL_RUNNER" ]]; then
  echo "ERROR: test runner not found at $EXTERNAL_RUNNER" >&2
  echo "  Set up the testing folder: git clone or create ../datawatch-testing/" >&2
  exit 1
fi

exec bash "$EXTERNAL_RUNNER" "$@"
