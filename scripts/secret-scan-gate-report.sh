#!/usr/bin/env bash
# secret-scan-gate-report.sh — counts-only report for the secret-scan gate.
#
# Runs the secret-scan test suites:
#
#   env -u TMUX go test ./internal/git/ ./internal/config/ -run SecretScan
#
# and prints exactly four lines on stdout, in this order:
#
#   findings_tests_passed: N  SecretScan-selected top-level tests that passed
#   detectors: N              built-in secret detectors (AllDetectors, internal/git)
#   allowlist_cases: N        allow-list test cases (Test*AllowList*) in the two packages
#   failures: N               SecretScan-selected tests that failed (0 = green;
#                             also 1 if the test command could not succeed at all)
#
# Guarantees by construction:
#   - stdout carries only the four count lines (no test output, no paths,
#     no secret material — test output is captured in a variable and only
#     ever machine-counted, never printed);
#   - every external tool used is resolved via `command -v` before use;
#   - runs under `env -u TMUX`; no gh, no daemon, no network;
#   - exits non-zero iff failures > 0.

set -u
LC_ALL=C
export LC_ALL

# --- resolve every external tool via command -v ----------------------------
for tool in go awk grep env dirname; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'error: required tool %s not found\n' "$tool" >&2
    exit 1
  fi
done

# --- work from the repository root (this script lives in scripts/) ----------
case "$(dirname -- "$0")" in
  */*) cd "$(dirname -- "$0")/.." || exit 1 ;;
esac
# The required sources must be reachable from here; otherwise fail quietly.
if [ ! -f internal/git/secretscan.go ] || [ ! -f internal/config/config.go ]; then
  exit 1
fi

# --- detectors: entries of `var AllDetectors` in internal/git ---------------
detectors=$(awk '
  /var AllDetectors = \[\]string\{/ { inblock = 1; next }
  inblock { if ($0 ~ /^\}/) exit; if (NF > 0 && $1 !~ /^\/\//) n++ }
  END { print n + 0 }
' internal/git/secretscan.go)

# --- allowlist_cases: Test*AllowList* functions in the two packages ---------
allowlist_cases=$(grep -rhcE '^func Test[A-Za-z0-9_]*AllowList' \
  internal/git internal/config 2>/dev/null \
  | awk '{ s += $1 } END { print s + 0 }')

# --- the test run (captured; never printed) ---------------------------------
testlog=$(env -u TMUX go test -count=1 -v -run SecretScan \
  ./internal/git/ ./internal/config/ 2>&1)
test_rc=$?

# Top-level test results start at column 0 ("--- PASS: TestX"); subtest
# results are indented, and test names never contain a slash here.
passed=$(printf '%s\n' "$testlog" \
  | awk '$1 == "---" && $2 == "PASS:" && $3 ~ /^Test[A-Za-z0-9_]*$/ { n++ } END { print n + 0 }')
failed=$(printf '%s\n' "$testlog" \
  | awk '$1 == "---" && $2 == "FAIL:" && $3 ~ /^Test[A-Za-z0-9_]*$/ { n++ } END { print n + 0 }')

# A non-zero exit with no parsed failure (build error, missing tool, etc.)
# still has to surface as a failure.
if [ "$test_rc" -ne 0 ] && [ "$failed" -eq 0 ]; then
  failed=1
fi

printf 'findings_tests_passed: %d\n' "$passed"
printf 'detectors: %d\n' "$detectors"
printf 'allowlist_cases: %d\n' "$allowlist_cases"
printf 'failures: %d\n' "$failed"

if [ "$failed" -gt 0 ]; then
  exit 1
fi
exit 0
