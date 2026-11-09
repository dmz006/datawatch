#!/usr/bin/env bash
# Tests for scripts/ci-install-trivy.sh. The script is sourced (its BASH_SOURCE
# guard keeps main from running) and a shell function named curl overrides the
# real one, so no test touches the network or the real install directory.
# Each case gets a fresh mktemp -d directory that is removed on EXIT.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$HERE/../ci-install-trivy.sh"

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
#   flaky     - attempts 1 and 2 fail with HTTP 504; attempt 3 writes a stub
#               install script into the -o target and succeeds
#   always504 - every attempt fails with HTTP 504
# Attempts are counted in $FAKE_CURL_DIR/calls; each invocation's argv is saved
# to $FAKE_CURL_DIR/args-<invocation>.
FAKE_CURL_MODE=""
FAKE_CURL_DIR=""

# _fake_attempt <attempt-number> <target-file>: one simulated transport attempt.
_fake_attempt() {
  local attempt="$1" target="$2"
  case "$FAKE_CURL_MODE" in
    flaky)
      if (( attempt <= 2 )); then
        echo "curl: (22) The requested URL returned error: 504" >&2
        return 22
      fi
      printf 'echo "stub-install args: $*"\n' > "$target"
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
    # Each simulated attempt is one call; keep the running total on disk.
    local total=0
    if [[ -f "$FAKE_CURL_DIR/calls" ]]; then
      total="$(cat "$FAKE_CURL_DIR/calls")"
    fi
    printf '%s' "$((total + 1))" > "$FAKE_CURL_DIR/calls"

    rc=0
    _fake_attempt "$attempt" "$target" "$@" || rc=$?
    if (( rc == 0 )) || (( attempt > retries )); then
      break
    fi
  done
  return "$rc"
}

# --- Case 1: transient 504s, then success ------------------------------------
case_retry_then_success() {
  local dir out rc calls url
  dir="$(mktemp -d)"
  # shellcheck disable=SC2064 # intentional: bake the path in now
  trap "rm -rf -- $(printf '%q' "$dir")" EXIT
  FAKE_CURL_DIR="$dir"
  FAKE_CURL_MODE="flaky"
  local install_dir="$dir/install"
  mkdir -p "$install_dir"

  set +e
  out="$(main "v0.66.0" "$install_dir" 2>&1)"
  rc=$?
  set -e

  calls="$(cat "$dir/calls")"
  url="$(grep -m1 'https://' "$dir/args-1" || true)"

  if [[ $rc -eq 0 ]]; then
    pass "case 1: script exits 0 after transient 504s"
  else
    fail "case 1: script exited $rc: $out"
  fi

  if [[ "$calls" == "3" ]]; then
    pass "case 1: fake curl was called exactly 3 times"
  else
    fail "case 1: fake curl called $calls times, expected 3"
  fi

  if [[ "$url" == *"/trivy/v0.66.0/"* && "$url" != *"/main/"* ]]; then
    pass "case 1: download URL names the version tag, not main"
  else
    fail "case 1: download URL was '$url'"
  fi

  if [[ "$out" == *"$install_dir"* && "$out" == *"v0.66.0"* ]]; then
    pass "case 1: stub received the install directory and version"
  else
    fail "case 1: stub output was '$out'"
  fi

  rm -rf -- "$dir"
  trap - EXIT
}

# --- Case 2: persistent 504s -------------------------------------------------
case_persistent_failure() {
  local dir install_dir err rc
  dir="$(mktemp -d)"
  # shellcheck disable=SC2064 # intentional: bake the path in now
  trap "rm -rf -- $(printf '%q' "$dir")" EXIT
  FAKE_CURL_DIR="$dir"
  FAKE_CURL_MODE="always504"
  install_dir="$dir/install"
  mkdir -p "$install_dir"

  set +e
  err="$(main "v0.66.0" "$install_dir" 2>&1 >/dev/null)"
  rc=$?
  set -e

  if [[ $rc -ne 0 ]]; then
    pass "case 2: script exits non-zero ($rc) on persistent 504s"
  else
    fail "case 2: script exited 0 on persistent 504s"
  fi

  if [[ "$err" == *"trivy/v0.66.0/contrib/install.sh"* && "$err" == *"curl exit status 22"* ]]; then
    pass "case 2: error names the URL and the curl exit status"
  else
    fail "case 2: error message was '$err'"
  fi

  if [[ -z "$(ls -A "$install_dir")" ]]; then
    pass "case 2: install directory is left empty"
  else
    fail "case 2: install directory is not empty: $(ls -A "$install_dir")"
  fi

  rm -rf -- "$dir"
  trap - EXIT
}

# --- Case 3: missing argument ------------------------------------------------
case_missing_argument() {
  local dir out rc calls
  dir="$(mktemp -d)"
  # shellcheck disable=SC2064 # intentional: bake the path in now
  trap "rm -rf -- $(printf '%q' "$dir")" EXIT
  FAKE_CURL_DIR="$dir"
  FAKE_CURL_MODE="always504"

  set +e
  out="$(main "v0.66.0" 2>&1)"
  rc=$?
  set -e

  calls="0"
  if [[ -f "$dir/calls" ]]; then
    calls="$(cat "$dir/calls")"
  fi

  if [[ $rc -eq 2 && "$out" == usage:* ]]; then
    pass "case 3: missing argument exits 2 with a usage line"
  else
    fail "case 3: exit $rc, output '$out'"
  fi

  if [[ "$calls" == "0" ]]; then
    pass "case 3: curl was not called"
  else
    fail "case 3: curl was called $calls times"
  fi

  rm -rf -- "$dir"
  trap - EXIT
}

# run_case <function>: each case runs in its own subshell so its EXIT trap owns
# its directory. The subshell reports its failure count through its exit status;
# a case that aborts outright (non-zero with no recorded failure) counts as one.
run_case() {
  local rc=0
  # The subshell resets its own counter; the parent tallies its exit status.
  # shellcheck disable=SC2030,SC2031 # intentional: subshell-local count
  ( FAILURES=0; "$1"; exit "$FAILURES" ) || rc=$?
  if (( rc > 0 )); then
    # shellcheck disable=SC2031 # the subshell's count is the exit status
    FAILURES=$((FAILURES + rc))
  fi
}
run_case case_retry_then_success
run_case case_persistent_failure
run_case case_missing_argument

if (( FAILURES > 0 )); then
  echo "$FAILURES check(s) failed" >&2
  exit 1
fi
echo "all trivy install script checks passed"
