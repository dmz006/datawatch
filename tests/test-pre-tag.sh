#!/usr/bin/env bash
# Tests scripts/pre-tag.sh: step order, stop-at-first-failure, skip message for a
# missing tool, record line format. Uses PRE_TAG_DRY_RUN=1 and PATH stubs only.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT/scripts/pre-tag.sh"
fails=0
ok()   { echo "ok   - $1"; }
bad()  { echo "FAIL - $1"; fails=$((fails+1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

EXPECT="fmt-check govet-check golangci-lint build mod-tidy go-test pwa-tests docs-sync howto-coverage internal-refs local-leaks version-bump tmux-kill dockerfile-go-version toolchain-pin trivy-single certifi accepted-risks trivyignore-regen govulncheck gosec gitleaks goreleaser-snapshot slow-opt-in docker-agent-base"

# 1. dry run prints every step id in order, writes no record
REC="$WORK/dry-record"
got="$(PRE_TAG_DRY_RUN=1 PRE_TAG_RECORD_FILE="$REC" bash "$SCRIPT" | sed -E 's/^\[([^]]+)\].*/\1/' | tr '\n' ' ' | sed 's/ $//')"
check "dry-run step order" '[ "$got" = "$EXPECT" ]'
check "dry-run writes no record" '[ ! -e "$REC" ]'

# 2. stubbed PATH: only basic tools linked, the rest are stubs
BIN="$WORK/bin"; mkdir -p "$BIN"
for t in bash env git date grep sed awk cat mkdir wc tr head tail sort dirname basename rm mktemp xargs uniq cut python3; do
  p="$(command -v "$t")" && ln -s "$p" "$BIN/$t"
done
LOG="$WORK/calls.log"
stub() { printf '#!/bin/bash\necho "%s $*" >> "%s"\n%s\n' "$1" "$LOG" "${2:-exit 0}" > "$BIN/$1"; chmod +x "$BIN/$1"; }
stub gofmt; stub node; stub make
stub go 'if [ "${1:-}" = vet ] && [ -n "${STUB_VET_FAIL:-}" ]; then exit 1; fi; exit 0'
# golangci-lint deliberately absent -> skip path

run() { PATH="$BIN" PRE_TAG_RECORD_FILE="$REC" bash "$SCRIPT" 2>&1; }

REC="$WORK/rec1"; : > "$LOG"
out="$(STUB_VET_FAIL=1 run)"; rc=$?
check "failure returns non-zero" '[ "$rc" -ne 0 ]'
check "fmt-check ran before vet" 'echo "$out" | grep -q "^\[fmt-check\] ok"'
check "govet-check failed" 'echo "$out" | grep -q "^\[govet-check\] FAIL"'
check "later steps did not run" '! echo "$out" | grep -q "^\[build\]"'
check "failing run stops before the skip step" '! echo "$out" | grep -q "golangci-lint"'
check "step shows elapsed seconds" 'echo "$out" | grep -qE "^\[fmt-check\] ok \([0-9]+s\)"'
check "record has one line" '[ "$(wc -l < "$REC")" -eq 1 ]'
re='^[0-9a-f]{40} [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]{8}Z steps=[0-9]+ result=fail seconds=[0-9]+$'
check "record line format (fail)" 'grep -Eq "$re" "$REC"'

# 3. a passing run (steps needing real repo tools may fail; use all-pass stubs for python3/bash scripts is
#    not possible, so assert the record gains a second line with a valid format regardless of result)
out2="$(run)"; 
check "second run appends a line" '[ "$(wc -l < "$REC")" -eq 2 ]'
re2='^[0-9a-f]{40} [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]{8}Z steps=[0-9]+ result=(pass|fail) seconds=[0-9]+$'
check "record line format (second)" 'tail -1 "$REC" | grep -Eq "$re2"'

check "skip message for missing tool" 'echo "$out2" | grep -qx "SKIP \[golangci-lint\]: golangci-lint not found"'

# 4. .gitignore covers the record
check "record is git-ignored" '(cd "$ROOT" && git check-ignore -q .datawatch/pre-tag-record)'

[ "$fails" -eq 0 ] && { echo "all pre-tag tests passed"; exit 0; }
echo "$fails failure(s)"; exit 1
