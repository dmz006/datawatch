#!/usr/bin/env bash
# Verification: the housekeeping how-tos are updated and the embedded copy is
# in sync. Runs `make sync-docs` FIRST (the embedded copy is the check target).
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

allowed="docs/howto/automaton-housekeeping-repairs.md
docs/howto/automaton-worktree-cleanup.md
internal/server/web/docs/howto/automaton-housekeeping-repairs.md
internal/server/web/docs/howto/automaton-worktree-cleanup.md
internal/autonomous/housekeeping_repairs.go
internal/autonomous/housekeeping_repair_advance.go
internal/autonomous/housekeeping_repairs_test.go
internal/autonomous/housekeeping_repair_advance_test.go
internal/autonomous/api_unlanded_forward.go
internal/autonomous/api_unlanded_forward_test.go
internal/server/unlanded_work_api.go
internal/server/unlanded_work_real_wrapper_test.go"

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

test -f docs/howto/automaton-housekeeping-repairs.md
test -s docs/howto/automaton-housekeeping-repairs.md
test -f docs/howto/automaton-worktree-cleanup.md
test -s docs/howto/automaton-worktree-cleanup.md

# --- docs sync FIRST, then the doc checks (embedded copy is check target) ---
make sync-docs

for h in automaton-housekeeping-repairs automaton-worktree-cleanup; do
  src="docs/howto/${h}.md"
  emb="internal/server/web/docs/howto/${h}.md"
  test -f "$emb" || { echo "missing embedded copy: $emb"; exit 1; }
  diff -u "$src" "$emb" || { echo "embedded copy drifted for $h"; exit 1; }
done

scripts/check-howto-coverage.sh

# No internal tracker IDs or version pinning in the changed how-tos.
! grep -nE 'BL[0-9]+|issue #[0-9]+|#gh-[0-9]+|v?[0-9]+\.[0-9]+\.[0-9]+' \
    docs/howto/automaton-housekeeping-repairs.md \
    docs/howto/automaton-worktree-cleanup.md
echo "hk-housekeeping-howto: PASS"
