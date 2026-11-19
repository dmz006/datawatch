#!/usr/bin/env bash
# Final gate for the cleanup-passes repair (read-only; changes no files).
#
# Checks (cumulative, always against the merge base with main):
#   1. gofmt cleanness of every Go file changed since the merge base
#      (changed files only, never a whole package or directory)
#   2. go build ./... (skipped with a warning when the tree has no go.mod)
#   3. for each test-name family Reaper, Worktree, TempSweep and RemoteReap:
#      a changed Go test file carries a test function whose name contains
#      that word, and `go test` over the packages holding the changed test
#      files passes
#   4. after `make sync-docs`: docs/howto/automation-cleanup-passes.md exists
#      and carries each exact term "branch reaper", "worktree cleanup",
#      "temp sweep", "remote reap", "report-only", "backed up", and the
#      embedded mirror internal/server/web/docs/howto/automation-cleanup-passes.md
#      exists when the internal/server/web/docs directory is present
#
# This gate never asserts on the whole changed-file set (rule
# final_gate_artefact_allowed). Generated artefacts the daemon writes per
# Automaton (changelog draft, parity draft, gate report) are exempt from the
# changed-file computation (rule artefact_files_allowed): docs/plans/drafts/,
# docs/plans/parity-issues/, docs/plans/reports/, and CHANGELOG.md.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1
fail=0

have_timeout=0
if command -v timeout >/dev/null 2>&1; then have_timeout=1; fi
run() { if [ "$have_timeout" -eq 1 ]; then timeout 1800 "$@"; else "$@"; fi }

# artefact_files_allowed: generated artefacts the daemon writes per Automaton.
is_artefact() {
  case "$1" in
    CHANGELOG.md|docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*|.datawatch/reports/*|.datawatch/verify/*)
      return 0 ;;
  esac
  return 1
}

BASE="$(git merge-base HEAD main 2>/dev/null || git merge-base HEAD origin/main 2>/dev/null || echo '')"
RAW=""
if [ -n "$BASE" ]; then
  RAW="$(git diff --name-only "$BASE" -- '*.go' 2>/dev/null)"
fi
UNTRACKED="$(git ls-files --others --exclude-standard -- '*.go' 2>/dev/null)"
GOFILES=""
while IFS= read -r p; do
  [ -n "$p" ] || continue
  is_artefact "$p" && continue
  GOFILES="$GOFILES $p"
done < <(printf '%s\n%s\n' "$RAW" "$UNTRACKED" | sort -u)

# 1) gofmt on the changed Go files only (never over a whole package/directory)
if [ -n "$GOFILES" ]; then
  BAD="$(gofmt -l $GOFILES 2>/dev/null || true)"
  if [ -n "$BAD" ]; then
    echo "FAIL: gofmt reports unformatted changed Go files:"
    printf '%s\n' "$BAD"
    fail=1
  fi
fi

# 2) build the module (only when this tree is a Go module). The embedded docs
#    index (internal/docsindex/assets) is gitignored, so generate it first.
if [ -f go.mod ]; then
  if command -v make >/dev/null 2>&1 && make -n docs-index >/dev/null 2>&1; then
    run make docs-index >/dev/null 2>&1 || echo "WARN: make docs-index failed"
  fi
  if ! run go build ./...; then
    echo "FAIL: go build ./..."
    fail=1
  fi
else
  echo "WARN: no go.mod at the repository root; skipping go build"
fi

# 3) one changed-test-file match per test-name family, then run the changed packages
TESTDIRS=""
for WORD in "func Test.*Reaper" "func Test.*Worktree" "func Test.*TempSweep" "func Test.*RemoteReap"; do
  FOUND=""
  for p in $GOFILES; do
    case "$p" in
      *_test.go)
        if [ -f "$p" ] && grep -qE "$WORD" "$p" 2>/dev/null; then
          FOUND="$FOUND $p"
        fi
        ;;
    esac
  done
  if [ -z "$FOUND" ]; then
    echo "FAIL: no changed Go test file carries a test function matching $WORD"
    fail=1
  else
    for p in $FOUND; do
      d="${p%/*}"
      [ "$d" = "$p" ] && d="."
      case " $TESTDIRS " in
        *" $d "*) ;;
        *) TESTDIRS="$TESTDIRS $d" ;;
      esac
    done
  fi
done
if [ "$fail" -eq 0 ] && [ -n "$TESTDIRS" ]; then
  PKGS=""
  for d in $TESTDIRS; do PKGS="$PKGS ./$d"; done
  OUT="$(run go test $PKGS 2>&1)" || {
    echo "FAIL: go test $TESTDIRS"
    printf '%s\n' "$OUT" | tail -n 60
    fail=1
  }
fi

# 4) docs: sync the embedded copy first, then check the howto and the mirror
if command -v make >/dev/null 2>&1 && make -n sync-docs >/dev/null 2>&1; then
  if ! run make sync-docs; then
    echo "FAIL: make sync-docs"
    fail=1
  fi
else
  echo "WARN: make target 'sync-docs' unavailable in this tree; skipping docs sync"
fi
H="docs/howto/automation-cleanup-passes.md"
if [ ! -f "$H" ]; then
  echo "FAIL: missing $H"
  fail=1
else
  for t in "branch reaper" "worktree cleanup" "temp sweep" "remote reap" "report-only" "backed up"; do
    if ! grep -q "$t" "$H" 2>/dev/null; then
      echo "FAIL: howto missing required term: $t"
      fail=1
    fi
  done
  if [ -d internal/server/web/docs ] && [ ! -f internal/server/web/docs/howto/automation-cleanup-passes.md ]; then
    echo "FAIL: embedded docs mirror missing the howto after sync"
    fail=1
  fi
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "PASS: final gate cleanup repair"
