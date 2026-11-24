#!/usr/bin/env bash
# Tests for scripts/check-no-absolute-symlink-targets.sh. Each case builds a
# scratch fixture repo under a fresh mktemp -d directory (removed on EXIT)
# with a committed symlink, then runs the checker against it through the
# CHECK_REPO override. The tests never run the checker against the real repo.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="$HERE/../check-no-absolute-symlink-targets.sh"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

FAILURES=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; FAILURES=$((FAILURES + 1)); }

REPO="$TMP_DIR/repo"

# expect <pass|fail> <name> — runs the checker against $REPO (CHECK_REPO).
expect() {
  local want="$1" name="$2" rc=0
  CHECK_REPO="$REPO" bash "$CHECK" >/dev/null 2>&1 || rc=$?
  if [[ "$want" == pass && $rc -eq 0 ]] || [[ "$want" == fail && $rc -ne 0 ]]; then
    pass "$name"
  else
    fail "$name (expected $want, exit $rc)"
  fi
}

# Case A: a tracked symlink whose stored target is an absolute path must fail.
rm -rf "$REPO"
git init -q -b main "$REPO"
mkdir -p "$REPO/scripts"
ln -s /nonexistent-absolute-target-x "$REPO/scripts/node_modules"
( cd "$REPO" && git add scripts/node_modules && git -c user.email=t@t -c user.name=t commit -qm t )
expect fail "case A: committed absolute symlink target is reported"

# Case B: a fresh fixture whose only tracked symlink target is relative must pass.
rm -rf "$REPO"
git init -q -b main "$REPO"
mkdir -p "$REPO/scripts"
echo '{}' > "$REPO/scripts/package.json"
ln -s ./package.json "$REPO/scripts/link"
( cd "$REPO" && git add scripts && git -c user.email=t@t -c user.name=t commit -qm t )
expect pass "case B: relative symlink target passes"

if (( FAILURES > 0 )); then
  echo "$FAILURES test(s) failed" >&2
  exit 1
fi
echo "All tests passed"
