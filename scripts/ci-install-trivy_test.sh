#!/usr/bin/env bash
# Tests for scripts/ci-install-trivy.sh. Sources the script and overrides
# curl and sh with shell functions, so no network access or real install runs.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$HERE/ci-install-trivy.sh"

FAILURES=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; FAILURES=$((FAILURES + 1)); }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- mocks -------------------------------------------------------------------
CURL_ARGS_FILE="$WORK/curl-args"
CURL_STATUS=0
curl() {
  printf '%s\n' "$@" > "$CURL_ARGS_FILE"
  # Find the -o target and write a fake install script into it.
  local prev="" arg
  for arg in "$@"; do
    if [[ "$prev" == "-o" ]]; then
      printf 'echo fake-install\n' > "$arg"
    fi
    prev="$arg"
  done
  return "$CURL_STATUS"
}

SH_ARGS_FILE="$WORK/sh-args"
SH_STDIN_FILE="$WORK/sh-stdin"
sh() {
  printf '%s\n' "$@" > "$SH_ARGS_FILE"
  cat > "$SH_STDIN_FILE"
}

# --- usage: missing / extra arguments ----------------------------------------
for argv in "" "v0.66.0" "v0.66.0 ''" "v0.66.0 /x extra"; do
  set +e
  out="$(eval "main $argv" 2>&1)"
  rc=$?
  set -e
  if [[ $rc -eq 2 && "$out" == usage:* ]]; then
    pass "usage with args [$argv] exits 2"
  else
    fail "usage with args [$argv] exited $rc: $out"
  fi
done

# --- success path -------------------------------------------------------------
CURL_STATUS=0
main "v0.66.0" "$WORK/bin"
if grep -qx 'https://raw.githubusercontent.com/aquasecurity/trivy/v0.66.0/contrib/install.sh' "$CURL_ARGS_FILE"; then
  pass "URL is built from the tag, not main"
else
  fail "unexpected curl args: $(cat "$CURL_ARGS_FILE")"
fi
if grep -qx -- '--retry' "$CURL_ARGS_FILE" && grep -qx -- '--retry-all-errors' "$CURL_ARGS_FILE" \
   && grep -qx -- '--max-time' "$CURL_ARGS_FILE" && grep -qx '120' "$CURL_ARGS_FILE"; then
  pass "curl uses the required retry and timeout flags"
else
  fail "curl flags missing from: $(cat "$CURL_ARGS_FILE")"
fi
expected_sh="$(printf '%s\n' -s -- -b "$WORK/bin" v0.66.0)"
if [[ "$(cat "$SH_ARGS_FILE")" == "$expected_sh" ]]; then
  pass "sh runs with -s -- -b <dir> <tag>"
else
  fail "unexpected sh args: $(cat "$SH_ARGS_FILE")"
fi
if grep -qx 'echo fake-install' "$SH_STDIN_FILE"; then
  pass "downloaded file is fed to sh on stdin"
else
  fail "sh stdin was not the downloaded file"
fi

# --- temp file removed on EXIT -------------------------------------------------
# Run main in a subshell so its EXIT trap fires, and record the temp path.
TMP_RECORD="$WORK/tmp-path"
# shellcheck disable=SC2329 # invoked by main via command lookup
mktemp() { local p; p="$(command mktemp "$WORK/tmpXXXX")"; printf '%s' "$p" > "$TMP_RECORD"; printf '%s\n' "$p"; }
( main "v0.66.0" "$WORK/bin" ) || true
unset -f mktemp
if [[ -s "$TMP_RECORD" && ! -e "$(cat "$TMP_RECORD")" ]]; then
  pass "temporary file is removed on exit"
else
  fail "temporary file was not removed"
fi

# --- curl failure ---------------------------------------------------------------
CURL_STATUS=22
set +e
err="$(main "v0.66.0" "$WORK/bin" 2>&1 >/dev/null)"
rc=$?
set -e
if [[ $rc -eq 22 && "$err" == *"curl exit status 22"* && "$err" == *"trivy/v0.66.0/contrib/install.sh"* ]]; then
  pass "curl failure exits with curl status and names the URL"
else
  fail "curl failure: rc=$rc err=$err"
fi
CURL_STATUS=0

if (( FAILURES > 0 )); then
  echo "$FAILURES test(s) failed" >&2
  exit 1
fi
echo "all tests passed"
