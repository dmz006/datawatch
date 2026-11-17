#!/usr/bin/env bash
# Verification: the advance_judge repair group reads each Automaton once per
# pass and words findings from a single consistent check (no repeated
# skipped entries per Automaton, no "every task finished" + "N not
# finished" on one finding).
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"

# The allowed set below includes how-to / embedded docs entries: sync the
# embedded docs FIRST so any doc comparison sees a fresh copy.
make sync-docs

BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"
trap 'rm -f "$CH"' EXIT
{
  git diff --name-only "$BASE"
  git ls-files --others --exclude-standard
} | sort -u > "$CH"

allowed="internal/autonomous/housekeeping_repairs.go
internal/autonomous/housekeeping_repair_advance.go
internal/autonomous/housekeeping_repairs_test.go
internal/autonomous/housekeeping_repair_advance_test.go
internal/autonomous/api_unlanded_forward.go
internal/autonomous/api_unlanded_forward_test.go
internal/server/unlanded_work_api.go
internal/server/unlanded_work_real_wrapper_test.go
docs/howto/automaton-housekeeping-repairs.md
docs/howto/automaton-worktree-cleanup.md
internal/server/web/docs/howto/automaton-housekeeping-repairs.md
internal/server/web/docs/howto/automaton-worktree-cleanup.md"

fail=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log) ;;
    *)
      if ! grep -Fxq -- "$f" <<< "$allowed"; then
        echo "unexpected changed file outside this Automaton's allowed set: $f"
        fail=1
      fi ;;
  esac
done < "$CH"
[ "$fail" -eq 0 ] || exit 1

if grep -q '\.go$' "$CH"; then
  bad="$(grep '\.go$' "$CH" | xargs gofmt -l)"
  if [ -n "$bad" ]; then echo "gofmt -l reports unformatted: $bad"; exit 1; fi
fi

# The mandated regression tests must exist.
grep -rq 'TestHousekeepingAdvanceWordingConsistent' internal/autonomous/housekeeping_repair_advance_test.go \
  || { echo "missing TestHousekeepingAdvanceWordingConsistent"; exit 1; }
grep -rq 'TestHousekeepingOneRecordPerSubject' internal/autonomous/housekeeping_repairs_test.go \
  || { echo "missing TestHousekeepingOneRecordPerSubject"; exit 1; }

go build ./...
go test ./internal/autonomous/ -count=1 -run 'Housekeeping'

echo "hk-advance-once-per-automaton: PASS"
