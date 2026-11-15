#!/usr/bin/env bash
# Tests scripts/trivy-scan-image.sh: docker save to a tar archive, trivy scan of
# that archive with the gate flags, exit-code passthrough, docker save failure,
# temp dir cleanup. Uses stub docker and trivy executables on a temp PATH only.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT/scripts/trivy-scan-image.sh"
fails=0
ok()   { echo "ok   - $1"; }
bad()  { echo "FAIL - $1"; fails=$((fails+1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

BIN="$WORK/bin"; mkdir -p "$BIN"
for t in bash env dirname mkdir mktemp rm cat grep sed; do
  p="$(command -v "$t")" && ln -s "$p" "$BIN/$t"
done
LOG="$WORK/calls.log"
stub() { printf '#!/bin/bash\necho "%s $*" >> "%s"\n%s\n' "$1" "$LOG" "${2:-exit 0}" > "$BIN/$1"; chmod +x "$BIN/$1"; }
# docker save -o <tar> <ref>: write the tar so trivy can see it existed
DOCKER_OK='if [ "$1" = save ] && [ "$2" = -o ]; then echo tar > "$3"; exit 0; fi; exit 1'
# trivy records whether its --input file exists at scan time
TRIVY_BODY='while [ $# -gt 0 ]; do [ "$1" = --input ] && { [ -f "$2" ] && echo "input-exists" >> "'"$LOG"'"; }; shift; done; exit ${STUB_TRIVY_RC:-0}'
stub docker "$DOCKER_OK"
stub trivy "$TRIVY_BODY"

TMPBASE="$WORK/scan-tmp"
run() { ( cd "$ROOT" && PATH="$BIN" TRIVY_SCAN_TMPDIR="$TMPBASE" bash "$SCRIPT" "$@" 2>&1 ); }

# 1. clean scan passes, invocations are correct
: > "$LOG"
out="$(STUB_TRIVY_RC=0 run example/image:pre-tag)"; rc=$?
check "clean scan exits 0" '[ "$rc" -eq 0 ]'
dline="$(grep '^docker ' "$LOG")"
check "docker save called with -o tar path and image ref" 'echo "$dline" | grep -Eq "^docker save -o $TMPBASE/trivy-scan\.[^ ]+/image\.tar example/image:pre-tag$"'
tarpath="$(echo "$dline" | sed -E 's/^docker save -o ([^ ]+) .*/\1/')"
tline="$(grep '^trivy ' "$LOG")"
check "trivy scans the saved tar via --input" 'echo "$tline" | grep -q -- "--input $tarpath "'
check "trivy --severity HIGH,CRITICAL" 'echo "$tline" | grep -q -- "--severity HIGH,CRITICAL"'
check "trivy --ignorefile .trivyignore" 'echo "$tline" | grep -q -- "--ignorefile .trivyignore "'
check "trivy --exit-code 1" 'echo "$tline" | grep -q -- "--exit-code 1"'
check "trivy --format table" 'echo "$tline" | grep -q -- "--format table"'
check "trivy subcommand is image" 'echo "$tline" | grep -q "^trivy image "'
check "tar existed when trivy ran" 'grep -qx "input-exists" "$LOG"'
check "temp dir removed after clean scan" '[ ! -e "$(dirname "$tarpath")" ] && [ -z "$(ls -A "$TMPBASE")" ]'

# 2. finding: trivy exits 1 -> helper exits 1 (gate fails)
: > "$LOG"
out="$(STUB_TRIVY_RC=1 run example/image:pre-tag)"; rc=$?
check "finding exits 1" '[ "$rc" -eq 1 ]'
check "temp dir removed after finding" '[ -z "$(ls -A "$TMPBASE")" ]'

# other trivy exit codes pass through unchanged
out="$(STUB_TRIVY_RC=4 run example/image:pre-tag)"; rc=$?
check "trivy exit code passed through unchanged" '[ "$rc" -eq 4 ]'

# 3. docker save fails -> non-zero, trivy not called, temp dir removed
stub docker 'exit 1'
: > "$LOG"
out="$(run example/missing:tag)"; rc=$?
check "failing docker save exits non-zero" '[ "$rc" -ne 0 ]'
check "failing docker save prints a clear message" 'echo "$out" | grep -q "docker save failed for example/missing:tag"'
check "trivy not called after docker save failure" '! grep -q "^trivy " "$LOG"'
check "temp dir removed after docker save failure" '[ -z "$(ls -A "$TMPBASE")" ]'
stub docker "$DOCKER_OK"

# 4. TRIVY_IGNOREFILE override (tests only); default stays .trivyignore
: > "$LOG"
( cd "$ROOT" && PATH="$BIN" TRIVY_SCAN_TMPDIR="$TMPBASE" TRIVY_IGNOREFILE="$WORK/ign" bash "$SCRIPT" img:x >/dev/null 2>&1 )
check "TRIVY_IGNOREFILE overrides the ignore file" 'grep "^trivy " "$LOG" | grep -q -- "--ignorefile $WORK/ign "'
check "default ignore file is .trivyignore in the source" 'grep -q "TRIVY_IGNOREFILE:-.trivyignore" "$SCRIPT"'

# 5. usage and missing tools
out="$(run)"; rc=$?
check "no argument prints usage and exits 2" '[ "$rc" -eq 2 ] && echo "$out" | grep -q "usage:"'
rm "$BIN/trivy"
out="$(run img:x)"; rc=$?
check "missing trivy exits 2" '[ "$rc" -eq 2 ] && echo "$out" | grep -q "trivy not found"'
stub trivy "$TRIVY_BODY"
rm "$BIN/docker"
out="$(run img:x)"; rc=$?
check "missing docker exits 2" '[ "$rc" -eq 2 ] && echo "$out" | grep -q "docker not found"'
stub docker "$DOCKER_OK"

# 6. default temp base is the repo's .datawatch/tmp; no network pull commands
check "default temp base is .datawatch/tmp" 'grep -q "/.datawatch/tmp}" "$SCRIPT"'
check "no docker pull in helper" '! grep -Eq "docker_bin\" pull|docker pull" "$SCRIPT"'
check "trap covers EXIT INT TERM" 'grep -q "^trap cleanup EXIT" "$SCRIPT" && grep -q "^trap .cleanup; exit 130. INT" "$SCRIPT" && grep -q "^trap .cleanup; exit 143. TERM" "$SCRIPT"'
check "helper is executable" '[ -x "$SCRIPT" ]'

[ "$fails" -eq 0 ] && { echo "all trivy-scan-image tests passed"; exit 0; }
echo "$fails failure(s)"; exit 1
