#!/usr/bin/env bash
# Verification: the autonomous API wrapper exposes the seven housekeeping
# manager methods (fix for the 503 "Housekeeping actions unavailable").
# Checks only the files this unit of work changes; compares against the
# merge base, never against one task's own file list.
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

fail=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    internal/autonomous/api_unlanded_forward.go|\
    internal/autonomous/api_unlanded_forward_test.go|\
    internal/server/unlanded_work_api.go|\
    internal/server/unlanded_work_real_wrapper_test.go|\
    internal/autonomous/housekeeping_repairs.go|\
    internal/autonomous/housekeeping_repair_advance.go|\
    internal/autonomous/housekeeping_repairs_test.go|\
    internal/autonomous/housekeeping_repair_advance_test.go|\
    docs/howto/automaton-housekeeping-repairs.md|\
    docs/howto/automaton-worktree-cleanup.md|\
    internal/server/web/docs/howto/automaton-housekeeping-repairs.md|\
    internal/server/web/docs/howto/automaton-worktree-cleanup.md|\
    .datawatch/*|.decompose-output-*.json|*.log)
      ;;
    *)
      echo "unexpected changed file outside this Automaton's allowed set: $f"
      fail=1 ;;
  esac
done < "$CH"
[ "$fail" -eq 0 ] || exit 1

# gofmt: only the changed Go files, never a package or directory.
if grep -q '\.go$' "$CH"; then
  bad="$(grep '\.go$' "$CH" | xargs gofmt -l)"
  if [ -n "$bad" ]; then echo "gofmt -l reports unformatted: $bad"; exit 1; fi
fi

test -f internal/autonomous/api_unlanded_forward.go

# The seven forwarding methods must exist on *API.
for m in UnlandedWork KeepUnlanded ArchiveUnlanded DeleteUnlandedWorktree \
         UncommittedWorkDiff CommitUncommittedWork FixPermissions; do
  grep -q "func (a \*API) ${m}(" internal/autonomous/api_unlanded_forward.go \
    || { echo "missing forwarding method: (*API).$m"; exit 1; }
done

go build ./...
go test ./internal/autonomous/ -count=1 -run 'TestAPIHousekeepingForward|Unlanded|Uncommitted'

echo "hk-api-forwarding: PASS"
