#!/usr/bin/env bash
# run-tests.sh — E2E test runner.
#
# Automatically creates a working directory outside the repo for each run so
# test artifacts (isolated daemon data, evidence, run logs) never touch the
# source tree. The directory is deleted on success; kept on failure so you can
# inspect evidence and resume.
#
# Usage:
#   bash scripts/run-tests.sh                       # full run
#   bash scripts/run-tests.sh --surface=api         # filter by surface
#   bash scripts/run-tests.sh --feature=sessions    # filter by feature
#   bash scripts/run-tests.sh --story=TS-042        # single story
#   bash scripts/run-tests.sh --resume-from=TS-042  # resume after a blocker
#
# Resuming a failed run (reuses its working dir so evidence is preserved):
#   DATAWATCH_TEST_ID=abc123 bash scripts/run-tests.sh --resume-from=TS-042
#
# Keep working dir even on success (for debugging):
#   KEEP_TEST_DIR=1 bash scripts/run-tests.sh

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO_PARENT=$(cd "$REPO_DIR/.." && pwd)

# --- working directory -------------------------------------------------------
# Each run gets a unique 6-char hex ID so parallel runs on the same filesystem
# don't collide. Set DATAWATCH_TEST_ID to reuse a specific prior run's dir
# (e.g. to resume after a failure).
RUN_ID="${DATAWATCH_TEST_ID:-$(openssl rand -hex 3)}"
TEST_DIR="$REPO_PARENT/datawatch-${RUN_ID}"
mkdir -p "$TEST_DIR"

FAILED=0
cleanup() {
  if [[ $FAILED -ne 0 || -n "${KEEP_TEST_DIR:-}" ]]; then
    echo ""
    echo "Working dir kept: $TEST_DIR"
    echo "  Resume: DATAWATCH_TEST_ID=$RUN_ID bash scripts/run-tests.sh --resume-from=<story>"
  else
    rm -rf "$TEST_DIR"
  fi
}
trap 'FAILED=$?; cleanup' EXIT

# --- port allocation --------------------------------------------------------
# Ask the OS for a free port on 127.0.0.1. Each call returns a different port
# so parallel runs never collide. Override via env vars if you need fixed ports.
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)'
}

# --- exports for story implementations --------------------------------------
export DATAWATCH_TEST_ID="$RUN_ID"
export DATAWATCH_TEST_DIR="$TEST_DIR"
export DATAWATCH_REPO_DIR="$REPO_DIR"
export DATAWATCH_COOKBOOK="$REPO_DIR/docs/testing/master-cookbook.md"

export TEST_RUN_HASH="$$"
export DATAWATCH_TEST_DATA="$TEST_DIR/.datawatch-test-${TEST_RUN_HASH}"
export TEST_PORT="${TEST_PORT:-$(free_port)}"
export TEST_TLS_PORT="${TEST_TLS_PORT:-$(free_port)}"

echo "Run ID  : $RUN_ID"
echo "Work dir: $TEST_DIR"
echo "Ports   : http=$TEST_PORT tls=$TEST_TLS_PORT"
echo ""

# --- argument parsing -------------------------------------------------------
FILTER_SURFACE=""
FILTER_FEATURE=""
FILTER_STORY=""
RESUME_FROM=""
FAIL_FAST=0

for arg in "$@"; do
  case "$arg" in
    --surface=*)   FILTER_SURFACE="${arg#*=}" ;;
    --feature=*)   FILTER_FEATURE="${arg#*=}" ;;
    --story=*)     FILTER_STORY="${arg#*=}" ;;
    --resume-from=*) RESUME_FROM="${arg#*=}" ;;
    --fail-fast*)  FAIL_FAST=1 ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

export FILTER_SURFACE FILTER_FEATURE FILTER_STORY RESUME_FROM FAIL_FAST

# --- story runner -----------------------------------------------------------
# Story implementations live in scripts/test-stories/ as individual scripts
# named TS-NNN.sh, sourced in order. Each script sets RESULT=pass|fail|skip
# and writes evidence to $DATAWATCH_TEST_DIR/evidence/TS-NNN/.

STORIES_DIR="$SCRIPT_DIR/test-stories"
PASS=0; FAIL=0; SKIP=0
EVIDENCE_DIR="$TEST_DIR/evidence"
mkdir -p "$EVIDENCE_DIR"

if [[ ! -d "$STORIES_DIR" ]]; then
  echo "No story implementations yet. Add scripts to scripts/test-stories/TS-NNN.sh"
  echo "Stories are defined in: $DATAWATCH_COOKBOOK"
  exit 0
fi

PAST_RESUME=0
[[ -z "$RESUME_FROM" ]] && PAST_RESUME=1

for story_script in "$STORIES_DIR"/TS-*.sh; do
  [[ -f "$story_script" ]] || continue
  story_id=$(basename "$story_script" .sh)

  # resume-from: skip stories before the resume point
  if [[ $PAST_RESUME -eq 0 ]]; then
    [[ "$story_id" == "$RESUME_FROM" ]] && PAST_RESUME=1 || continue
  fi

  # filters
  [[ -n "$FILTER_STORY" && "$story_id" != "$FILTER_STORY" ]] && continue

  RESULT=""
  mkdir -p "$EVIDENCE_DIR/$story_id"
  # shellcheck source=/dev/null
  source "$story_script"

  case "$RESULT" in
    pass) ((PASS++)); echo "✓ $story_id" ;;
    skip) ((SKIP++)); echo "- $story_id (skip)" ;;
    fail|*)
      ((FAIL++))
      echo "✗ $story_id"
      if [[ $FAIL_FAST -eq 1 ]]; then
        echo "Stopping at first failure (--fail-fast)."
        FAILED=1
        exit 1
      fi
      ;;
  esac
done

echo ""
echo "Results: $PASS passed  $FAIL failed  $SKIP skipped"

if [[ $FAIL -gt 0 ]]; then
  FAILED=1
  exit 1
fi
