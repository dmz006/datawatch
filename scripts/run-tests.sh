#!/usr/bin/env bash
# run-tests.sh — forwarding wrapper.
#
# The test runner lives outside this repo in a sibling folder named
# ../datawatch-<id>/ where <id> is a short 6-char hex identifier unique to
# each test environment. Using a unique ID lets multiple test environments
# (different machines, parallel CI runs, or local experiments) coexist on the
# same shared filesystem without conflicting.
#
# Resolution order:
#   1. DATAWATCH_TEST_ID env var  →  ../datawatch-${DATAWATCH_TEST_ID}/
#   2. Auto-discover: glob ../datawatch-*/run-tests.sh; use if exactly one match
#   3. Error with setup instructions
#
# Usage: all flags are forwarded verbatim to the external runner.
#   bash scripts/run-tests.sh [--surface=api] [--feature=sessions] ...
#   DATAWATCH_TEST_ID=abc123 bash scripts/run-tests.sh --story=TS-042
#
# Setup:
#   id=$(openssl rand -hex 3)            # generates e.g. "a3f9c1"
#   mkdir -p "../datawatch-${id}"
#   # copy or clone run-tests.sh there
#   export DATAWATCH_TEST_ID="$id"       # or add to your shell profile

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_PARENT="$SCRIPT_DIR/../.."

if [[ -n "$DATAWATCH_TEST_ID" ]]; then
  EXTERNAL_RUNNER="$REPO_PARENT/datawatch-${DATAWATCH_TEST_ID}/run-tests.sh"
  if [[ ! -f "$EXTERNAL_RUNNER" ]]; then
    echo "ERROR: DATAWATCH_TEST_ID=$DATAWATCH_TEST_ID but runner not found at $EXTERNAL_RUNNER" >&2
    exit 1
  fi
else
  # Auto-discover: find all sibling datawatch-<id>/run-tests.sh
  mapfile -t CANDIDATES < <(ls "$REPO_PARENT"/datawatch-*/run-tests.sh 2>/dev/null)
  if [[ ${#CANDIDATES[@]} -eq 0 ]]; then
    echo "ERROR: no test runner found. Set up a test folder:" >&2
    echo "  id=\$(openssl rand -hex 3)" >&2
    echo "  mkdir -p \"../datawatch-\${id}\"" >&2
    echo "  # copy or symlink run-tests.sh there" >&2
    echo "  export DATAWATCH_TEST_ID=\"\$id\"" >&2
    exit 1
  elif [[ ${#CANDIDATES[@]} -gt 1 ]]; then
    echo "ERROR: multiple test runners found; set DATAWATCH_TEST_ID to choose:" >&2
    for c in "${CANDIDATES[@]}"; do
      id=$(basename "$(dirname "$c")" | sed 's/^datawatch-//')
      echo "  DATAWATCH_TEST_ID=$id" >&2
    done
    exit 1
  fi
  EXTERNAL_RUNNER="${CANDIDATES[0]}"
fi

exec bash "$EXTERNAL_RUNNER" "$@"
