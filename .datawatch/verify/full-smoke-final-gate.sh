#!/usr/bin/env bash
# Verification for: full-smoke-final-gate. Compares against the merge base; checks only changed files.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 1
make sync-docs >/dev/null 2>&1 || { echo "FAIL: make sync-docs"; exit 1; }
base=$(git merge-base HEAD main) || exit 1
changed=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u)
fail=0
bad() { echo "FAIL: $*"; fail=1; }
for f in $changed; do
  case "$f" in *.sh) [[ -f "$f" ]] && { bash -n "$f" || bad "syntax $f"; } ;; esac
  case "$f" in *.js|*.mjs) [[ -f "$f" ]] && { node --check "$f" || bad "node --check $f"; } ;; esac
done
gofiles=$(echo "$changed" | grep '\.go$' | while read -r f; do [[ -f "$f" ]] && echo "$f"; done)
if [[ -n "$gofiles" ]]; then
  out=$(gofmt -l $gofiles); [[ -z "$out" ]] || bad "gofmt: $out"
  go build ./... || bad "go build"
  pk=$(echo "$gofiles" | xargs -n1 dirname | sort -u | sed 's|^|./|')
  go vet $pk || bad "go vet"
fi
# no section may be made to pass by skipping: no added SMOKE_ONLY-style bypass lines
if git diff "$base" -- scripts/release-smoke.sh | grep -E '^\+' | grep -Eq 'SMOKE_(PRIORITY_SOURCE|RESTART_MATRIX)[:=-]+.?0'; then bad "smoke edit adds a default-off switch"; fi
mkdir -p .datawatch/reports
T=$(command -v timeout || true)
# The release version bump belongs to the merge (the version_bump_left_to_merge task); the merge gates run the version check.
DW_SKIP_VERSION_BUMP_CHECK=1 env -u SMOKE_ONLY ${T:+$T 3600} bash scripts/release-smoke.sh >.datawatch/reports/full-smoke.log 2>&1; rc=$?
tail -30 .datawatch/reports/full-smoke.log
[[ $rc -eq 0 ]] || bad "full smoke rc=$rc"
grep -Eq "^ *FAIL " .datawatch/reports/full-smoke.log && bad "FAIL lines present"
[[ $fail -eq 0 ]] && echo "OK" ; exit $fail
