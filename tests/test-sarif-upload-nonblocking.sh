#!/usr/bin/env bash
#
# test-sarif-upload-nonblocking.sh
#
# Tests scripts/check-sarif-upload-nonblocking.py against fixture workflow
# directories built in one temporary directory, and against the real
# .github/workflows directory:
#
#   (a) good: four continue-on-error upload steps, a blocking baseline gate,
#       a goreleaser job that needs gosec -> exit 0.
#   (b) no-continue: an upload step without continue-on-error -> exit 1, R1.
#   (c) one-attempt: a job with a single upload step -> exit 1, R2.
#   (d) publish-blocking: blocking uploads upstream of a publishing job
#       -> exit 1, R3.
#   (e) gate-soft: the baseline gate sets continue-on-error -> exit 1, R4.
#   (f) the real workflows directory -> exit 0.
#   (g) a path that does not exist -> exit 2.
#
# Every fixture lives in one mktemp -d directory removed by a trap on EXIT;
# nothing is written into the repository.
#
# Usage: bash tests/test-sarif-upload-nonblocking.sh
# Output: one 'ok <case>' or 'FAIL <case> - <reason>' line per check, then a
#         final PASS or FAIL line; exits non-zero if any check failed.

set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$root/scripts/check-sarif-upload-nonblocking.py"

py="$(command -v python3)" || {
  echo "FAIL environment - python3 is not on PATH"
  echo "FAIL (1 check(s) failed)"
  exit 1
}
[ -r "$checker" ] || {
  echo "FAIL environment - checker is not readable: $checker"
  echo "FAIL (1 check(s) failed)"
  exit 1
}

tmp="$(mktemp -d)" || {
  echo "FAIL environment - mktemp -d failed"
  echo "FAIL (1 check(s) failed)"
  exit 1
}
trap 'rm -rf "$tmp"' EXIT

fails=0
ok() { printf 'ok   %s\n' "$1"; }
bad() {
  printf 'FAIL %s - %s\n' "$1" "$2"
  fails=$((fails + 1))
}

# expect <case> <want-exit> <want-rule-or-empty> <path>
expect() {
  local name="$1" want="$2" rule="$3" path="$4" out rc
  out="$("$py" "$checker" "$path" 2>&1)"
  rc=$?
  if [ "$rc" -ne "$want" ]; then
    bad "$name" "exit $rc, want $want: $(printf '%s' "$out" | tr '\n' ' ')"
  elif [ -n "$rule" ] && ! printf '%s\n' "$out" | grep -q "$rule"; then
    bad "$name" "output does not mention $rule: $(printf '%s' "$out" | tr '\n' ' ')"
  else
    ok "$name"
  fi
}

# (a) good
mkdir -p "$tmp/good"
cat > "$tmp/good/release.yaml" <<'YAML'
name: release
on:
  push:
    tags: ["v*"]
jobs:
  gosec:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Compare against the gosec baseline
        run: |
          python3 - <<'PY'
          import sys
          sys.exit(1)
          PY
      - name: Upload SARIF attempt 1
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 2
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 3
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 4
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
  goreleaser:
    needs: gosec
    runs-on: ubuntu-latest
    steps:
      - uses: goreleaser/goreleaser-action@v6
YAML
expect "good" 0 "" "$tmp/good"

# (b) no-continue
mkdir -p "$tmp/no-continue"
cat > "$tmp/no-continue/scan.yaml" <<'YAML'
name: scan
on: [push]
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v3
YAML
expect "no-continue" 1 "R1" "$tmp/no-continue"

# (c) one-attempt
mkdir -p "$tmp/one-attempt"
cat > "$tmp/one-attempt/scan.yaml" <<'YAML'
name: scan
on: [push]
jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
YAML
expect "one-attempt" 1 "R2" "$tmp/one-attempt"

# (d) publish-blocking
mkdir -p "$tmp/publish-blocking"
cat > "$tmp/publish-blocking/release.yaml" <<'YAML'
name: release
on:
  push:
    tags: ["v*"]
jobs:
  gosec:
    runs-on: ubuntu-latest
    steps:
      - name: Upload SARIF attempt 1
        uses: github/codeql-action/upload-sarif@v3
      - name: Upload SARIF attempt 2
        uses: github/codeql-action/upload-sarif@v3
      - name: Upload SARIF attempt 3
        uses: github/codeql-action/upload-sarif@v3
      - name: Upload SARIF attempt 4
        uses: github/codeql-action/upload-sarif@v3
  goreleaser:
    needs: gosec
    runs-on: ubuntu-latest
    steps:
      - uses: goreleaser/goreleaser-action@v6
YAML
expect "publish-blocking" 1 "R3" "$tmp/publish-blocking"

# (e) gate-soft
mkdir -p "$tmp/gate-soft"
cat > "$tmp/gate-soft/release.yaml" <<'YAML'
name: release
on:
  push:
    tags: ["v*"]
jobs:
  gosec:
    runs-on: ubuntu-latest
    steps:
      - name: Compare against the gosec baseline
        continue-on-error: true
        run: |
          python3 - <<'PY'
          import sys
          sys.exit(1)
          PY
      - name: Upload SARIF attempt 1
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 2
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 3
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
      - name: Upload SARIF attempt 4
        uses: github/codeql-action/upload-sarif@v3
        continue-on-error: true
  goreleaser:
    needs: gosec
    runs-on: ubuntu-latest
    steps:
      - uses: goreleaser/goreleaser-action@v6
YAML
expect "gate-soft" 1 "R4" "$tmp/gate-soft"

# (f) real workflows
expect "real-workflows" 0 "" "$root/.github/workflows"

# (g) missing path
expect "missing-path" 2 "" "$tmp/does-not-exist"

if [ "$fails" -eq 0 ]; then
  echo "PASS"
else
  echo "FAIL ($fails check(s) failed)"
  exit 1
fi
