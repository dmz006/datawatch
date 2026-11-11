#!/usr/bin/env bash
# Tests for scripts/check-trivy-version-single.sh. Each case builds a fixture
# tree under a fresh mktemp -d directory (removed on EXIT) holding
# scripts/trivy-version.txt and .github/workflows, then runs the checker
# against it through its repo-root argument.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="$HERE/../check-trivy-version-single.sh"

FAILURES=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; FAILURES=$((FAILURES + 1)); }

TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

N=0
# new_tree: create a clean fixture tree; prints its path.
new_tree() {
  N=$((N + 1))
  local d="$TMPROOT/t$N"
  mkdir -p "$d/scripts" "$d/.github/workflows"
  echo "v0.73.0" > "$d/scripts/trivy-version.txt"
  cat > "$d/.github/workflows/good.yaml" <<'YAML'
jobs:
  scan:
    steps:
      - run: scripts/ci-install-trivy.sh "$(cat scripts/trivy-version.txt)"
YAML
  printf '%s' "$d"
}

# expect <pass|fail> <name> <tree>
expect() {
  local want="$1" name="$2" tree="$3" rc=0
  "$CHECK" "$tree" >/dev/null 2>&1 || rc=$?
  if [[ "$want" == pass && $rc -eq 0 ]] || [[ "$want" == fail && $rc -ne 0 ]]; then
    pass "$name"
  else
    fail "$name (expected $want, exit $rc)"
  fi
}

t=$(new_tree)
expect pass "clean tree passes" "$t"

t=$(new_tree)
cat > "$t/.github/workflows/own.yaml" <<'YAML'
env:
  TRIVY_VERSION: v0.70.0
YAML
expect fail "workflow with its own TRIVY_VERSION fails" "$t"

t=$(new_tree)
cat > "$t/.github/workflows/literal.yaml" <<'YAML'
jobs:
  scan:
    steps:
      - run: scripts/ci-install-trivy.sh v0.70.0  # scripts/trivy-version.txt
YAML
expect fail "literal version tag to ci-install-trivy.sh fails" "$t"

t=$(new_tree)
cat > "$t/.github/workflows/noread.yaml" <<'YAML'
jobs:
  scan:
    steps:
      - run: scripts/ci-install-trivy.sh "$VER"
YAML
expect fail "install step not reading the version file fails" "$t"

t=$(new_tree)
rm "$t/scripts/trivy-version.txt"
expect fail "missing version file fails" "$t"

t=$(new_tree)
echo "0.73" > "$t/scripts/trivy-version.txt"
expect fail "malformed version file fails" "$t"

t=$(new_tree)
printf 'v0.73.0\nv0.74.0\n' > "$t/scripts/trivy-version.txt"
expect fail "multi-line version file fails" "$t"

if (( FAILURES > 0 )); then
  echo "$FAILURES test(s) failed" >&2
  exit 1
fi
echo "All tests passed"
