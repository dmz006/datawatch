#!/usr/bin/env bash
# Tests for scripts/ci-install-helm.sh. The script is sourced (its BASH_SOURCE
# guard keeps main from running) and a shell function named curl overrides the
# real one, so no test touches the network or the real install directory.
# Each case runs in its own subshell with a fresh mktemp -d directory that is
# removed on EXIT.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$HERE/../ci-install-helm.sh"

FAILURES=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; FAILURES=$((FAILURES + 1)); }

# --- fake curl ---------------------------------------------------------------
# The install script makes ONE curl invocation per run and relies on curl's own
# --retry N flag for retries. The fake therefore emulates curl's retry loop:
# when --retry N is present it makes up to N+1 transport attempts, and every
# attempt is one counted "call". The fake never sleeps (no --retry-delay wait).
#
# Behaviour is driven by FAKE_CURL_MODE:
#   flaky     - attempts 1 and 2 fail with HTTP 504; attempt 3 copies the
#               fixture archive ($FAKE_CURL_ARCHIVE) into the -o target
#   ok        - the first attempt copies the fixture archive and succeeds
#   always504 - every attempt fails with HTTP 504
# Attempts are counted in $FAKE_CURL_DIR/calls; each invocation's argv is saved
# to $FAKE_CURL_DIR/args-<invocation>.
FAKE_CURL_MODE=""
FAKE_CURL_DIR=""
FAKE_CURL_ARCHIVE=""

# make_helm_archive <work-dir> <archive-path>: build a small real tar.gz that
# holds linux-amd64/helm, a stub shell script standing in for the binary.
make_helm_archive() {
  local work="$1" archive="$2"
  mkdir -p "$work/linux-amd64"
  printf '#!/bin/sh\necho "stub helm"\n' > "$work/linux-amd64/helm"
  chmod 0755 "$work/linux-amd64/helm"
  tar -czf "$archive" -C "$work" linux-amd64/helm
}

# _fake_attempt <attempt-number> <target-file>: one simulated transport attempt.
_fake_attempt() {
  local attempt="$1" target="$2"
  case "$FAKE_CURL_MODE" in
    flaky)
      if (( attempt <= 2 )); then
        echo "curl: (22) The requested URL returned error: 504" >&2
        return 22
      fi
      cp -- "$FAKE_CURL_ARCHIVE" "$target"
      return 0
      ;;
    ok)
      cp -- "$FAKE_CURL_ARCHIVE" "$target"
      return 0
      ;;
    always504)
      echo "curl: (22) The requested URL returned error: 504" >&2
      return 22
      ;;
    *)
      echo "fake curl: unknown FAKE_CURL_MODE '$FAKE_CURL_MODE'" >&2
      return 99
      ;;
  esac
}

curl() {
  local n=0 attempt=0 retries=0 rc=0 arg prev="" target=""
  if [[ -f "$FAKE_CURL_DIR/invocations" ]]; then
    n="$(cat "$FAKE_CURL_DIR/invocations")"
  fi
  n=$((n + 1))
  printf '%s' "$n" > "$FAKE_CURL_DIR/invocations"
  printf '%s\n' "$@" > "$FAKE_CURL_DIR/args-$n"

  for arg in "$@"; do
    case "$prev" in
      -o) target="$arg" ;;
      --retry) retries="$arg" ;;
    esac
    prev="$arg"
  done

  # Emulate curl's internal retry: 1 initial attempt plus up to $retries more.
  while :; do
    attempt=$((attempt + 1))
    local total=0
    if [[ -f "$FAKE_CURL_DIR/calls" ]]; then
      total="$(cat "$FAKE_CURL_DIR/calls")"
    fi
    printf '%s' "$((total + 1))" > "$FAKE_CURL_DIR/calls"

    rc=0
    _fake_attempt "$attempt" "$target" || rc=$?
    if (( rc == 0 )) || (( attempt > retries )); then
      break
    fi
  done
  return "$rc"
}

# new_case_dir: create the case's scratch directory, remove it on EXIT, and
# point the fake curl at it. Sets CASE_DIR (callers run in their own subshell).
new_case_dir() {
  CASE_DIR="$(mktemp -d)"
  # shellcheck disable=SC2064 # intentional: bake the path in now
  trap "rm -rf -- $(printf '%q' "$CASE_DIR")" EXIT
  FAKE_CURL_DIR="$CASE_DIR"
  mkdir -p "$CASE_DIR/fixture"
  FAKE_CURL_ARCHIVE="$CASE_DIR/helm.tar.gz"
  make_helm_archive "$CASE_DIR/fixture" "$FAKE_CURL_ARCHIVE"
}

# --- Case 1: transient failures, then success --------------------------------
case_retry_then_success() {
  local out rc calls install_dir
  new_case_dir
  FAKE_CURL_MODE="flaky"
  install_dir="$CASE_DIR/install"
  mkdir -p "$install_dir"

  set +e
  out="$(main "v3.17.3" "$install_dir" 2>&1)"
  rc=$?
  set -e

  calls="0"
  if [[ -f "$CASE_DIR/calls" ]]; then
    calls="$(cat "$CASE_DIR/calls")"
  fi

  if [[ $rc -eq 0 ]]; then
    pass "case 1: script exits 0 after transient failures"
  else
    fail "case 1: script exited $rc: $out"
  fi

  if [[ "$calls" == "3" ]]; then
    pass "case 1: fake curl made exactly 3 attempts"
  else
    fail "case 1: fake curl made $calls attempts, expected 3"
  fi

  if [[ -f "$install_dir/helm" && -x "$install_dir/helm" ]]; then
    pass "case 1: helm is present and executable in the install directory"
  else
    fail "case 1: helm missing or not executable in $install_dir: $(ls -A "$install_dir")"
  fi
}

# --- Case 2: persistent failure ----------------------------------------------
case_persistent_failure() {
  local install_dir err rc
  new_case_dir
  FAKE_CURL_MODE="always504"
  install_dir="$CASE_DIR/install"
  mkdir -p "$install_dir"

  set +e
  err="$(main "v3.17.3" "$install_dir" 2>&1 >/dev/null)"
  rc=$?
  set -e

  if [[ $rc -ne 0 ]]; then
    pass "case 2: script exits non-zero ($rc) on persistent failure"
  else
    fail "case 2: script exited 0 on persistent failure"
  fi

  if [[ "$err" == *"helm-v3.17.3-linux-amd64.tar.gz"* && "$err" == *"curl exit status"* ]]; then
    pass "case 2: error names the URL and the curl exit status"
  else
    fail "case 2: error message was '$err'"
  fi

  if [[ -z "$(ls -A "$install_dir")" ]]; then
    pass "case 2: install directory is left empty"
  else
    fail "case 2: install directory is not empty: $(ls -A "$install_dir")"
  fi
}

# --- Case 3: the pinned version appears in the URL ---------------------------
case_pinned_version_in_url() {
  local install_dir url rc
  new_case_dir
  FAKE_CURL_MODE="ok"
  install_dir="$CASE_DIR/install"
  mkdir -p "$install_dir"

  set +e
  main "v3.17.3" "$install_dir" >/dev/null 2>&1
  rc=$?
  set -e

  url="$(grep -m1 'https://' "$CASE_DIR/args-1" || true)"

  if [[ $rc -eq 0 ]]; then
    pass "case 3: script exits 0 when the fetch succeeds"
  else
    fail "case 3: script exited $rc"
  fi

  if [[ "$url" == *"helm-v3.17.3-linux-amd64.tar.gz"* ]]; then
    pass "case 3: URL names the pinned release archive"
  else
    fail "case 3: URL was '$url'"
  fi

  if [[ "$url" != *"get-helm-3"* && "$url" != *"main"* ]]; then
    pass "case 3: URL has no get-helm-3 and no main"
  else
    fail "case 3: URL points at a moving target: '$url'"
  fi
}

# --- Case 4: missing argument ------------------------------------------------
case_missing_argument() {
  local out rc calls
  new_case_dir
  FAKE_CURL_MODE="always504"

  set +e
  out="$(main "v3.17.3" 2>&1)"
  rc=$?
  set -e

  calls="0"
  if [[ -f "$CASE_DIR/calls" ]]; then
    calls="$(cat "$CASE_DIR/calls")"
  fi

  if [[ $rc -eq 2 && "$out" == usage:* ]]; then
    pass "case 4: missing argument exits 2 with a usage line"
  else
    fail "case 4: exit $rc, output '$out'"
  fi

  if [[ "$calls" == "0" ]]; then
    pass "case 4: curl was never called"
  else
    fail "case 4: curl was called $calls times"
  fi
}

# run_case <function>: each case runs in its own subshell so its EXIT trap owns
# its directory. The subshell reports its failure count through its exit status;
# a case that aborts outright (non-zero with no recorded failure) counts as one.
run_case() {
  local rc=0
  # shellcheck disable=SC2030,SC2031 # intentional: subshell-local count
  ( FAILURES=0; "$1"; exit "$FAILURES" ) || rc=$?
  if (( rc > 0 )); then
    # shellcheck disable=SC2031 # the subshell's count is the exit status
    FAILURES=$((FAILURES + rc))
  fi
}
run_case case_retry_then_success
run_case case_persistent_failure
run_case case_pinned_version_in_url
run_case case_missing_argument

if (( FAILURES > 0 )); then
  echo "$FAILURES check(s) failed" >&2
  exit 1
fi
echo "all helm install script checks passed"
