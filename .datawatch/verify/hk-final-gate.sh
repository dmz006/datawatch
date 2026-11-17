#!/usr/bin/env bash
# Final gate (read-only): the whole housekeeping-fix change set — wrapper
# forwarding, the live-server gate, the advance_judge once-per-Automaton
# read, and the how-tos with a fresh embedded docs sync.
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"

BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"
trap 'rm -f "$CH"' EXIT
{
  git diff --name-only "$BASE"
  git ls-files --others --exclude-standard
} | sort -u > "$CH"

allowed="internal/autonomous/api_unlanded_forward.go
internal/autonomous/api_unlanded_forward_test.go
internal/autonomous/housekeeping_repairs.go
internal/autonomous/housekeeping_repair_advance.go
internal/autonomous/housekeeping_repairs_test.go
internal/autonomous/housekeeping_repair_advance_test.go
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
    .datawatch/*|.decompose-output-*.json|*.log|docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*) ;;
    *)
      if ! grep -Fxq -- "$f" <<< "$allowed"; then
        echo "unexpected changed file outside this Automaton's allowed set: $f"
        fail=1
      fi ;;
  esac
done < "$CH"
[ "$fail" -eq 0 ] || { echo "hk-final-gate: FAIL (file set)"; exit 1; }

# gofmt: only the changed Go files, never a package or directory.
if grep -q '\.go$' "$CH"; then
  bad="$(grep '\.go$' "$CH" | xargs gofmt -l)"
  if [ -n "$bad" ]; then
    echo "hk-final-gate: FAIL (gofmt: $bad)"; exit 1
  fi
fi

go build ./...

# Unit + integration tests for both touched packages, scoped to the
# housekeeping surface.
go test ./internal/autonomous/ -count=1 -run 'Housekeeping|Unlanded|Uncommitted|TestAPIHousekeepingForward'
go test ./internal/server/ -count=1 -run 'Unlanded|Uncommitted|FixPermissions'

# Docs: sync FIRST, then verify the embedded copy and howto coverage.
make sync-docs
for h in automaton-housekeeping-repairs automaton-worktree-cleanup; do
  diff -u "docs/howto/${h}.md" "internal/server/web/docs/howto/${h}.md" \
    || { echo "hk-final-gate: FAIL (embedded copy drifted: $h)"; exit 1; }
done
scripts/check-howto-coverage.sh

echo "hk-final-gate: PASS"
