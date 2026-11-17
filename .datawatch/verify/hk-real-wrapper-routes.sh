#!/usr/bin/env bash
# Verification: the real autonomous API wrapper (what cmd/datawatch/main.go
# passes to the server) satisfies the housekeeping gate and every one of the
# seven routes is served instead of 503.
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

allowed="internal/autonomous/api_unlanded_forward.go
internal/autonomous/api_unlanded_forward_test.go
internal/server/unlanded_work_api.go
internal/server/unlanded_work_real_wrapper_test.go
internal/autonomous/housekeeping_repairs.go
internal/autonomous/housekeeping_repair_advance.go
internal/autonomous/housekeeping_repairs_test.go
internal/autonomous/housekeeping_repair_advance_test.go
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

# Compile-time assertion in the server package (non-test file).
grep -rF 'var _ unlandedWorkOps = (*autonomous.API)(nil)' internal/server/*.go \
  | grep -qv '_test.go' \
  || { echo "missing compile-time assertion in internal/server"; exit 1; }

# The test must use the real wrapper, not a fake.
test -f internal/server/unlanded_work_real_wrapper_test.go
grep -q 'TestRealAPIWrapperServesHousekeepingRoutes' internal/server/unlanded_work_real_wrapper_test.go \
  || { echo "missing TestRealAPIWrapperServesHousekeepingRoutes"; exit 1; }
grep -q 'autonomous.NewManager' internal/server/unlanded_work_real_wrapper_test.go \
  || { echo "test does not build a real Manager"; exit 1; }
grep -q 'autonomous.NewAPI' internal/server/unlanded_work_real_wrapper_test.go \
  || { echo "test does not wrap the Manager with autonomous.NewAPI"; exit 1; }
grep -q 'Housekeeping actions unavailable' internal/server/unlanded_work_real_wrapper_test.go \
  || { echo "test does not assert the 503 body is gone"; exit 1; }

go build ./...
go test ./internal/autonomous/ -count=1 -run 'TestAPIHousekeepingForward'
go test ./internal/server/ -count=1 -run 'Unlanded|Uncommitted|FixPermissions'

echo "hk-real-wrapper-routes: PASS"
