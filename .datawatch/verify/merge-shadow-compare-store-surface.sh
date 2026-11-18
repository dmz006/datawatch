#!/usr/bin/env bash
# Verification: the post-ship hook stores the comparison itself (in addition
# to the defect candidates), and an Automaton without a stored plan gets a
# recorded reason that every status read (REST, MCP, CLI, comm channel, PWA)
# can surface instead of silence.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

env -u TMUX -u TMUX_TMPDIR bash -c '
set -euo pipefail

unformatted=$(gofmt -l \
  internal/mergeshadow/compare_store.go \
  internal/mergeshadow/plan_missing.go \
  internal/mergeshadow/plan_missing_test.go \
  internal/autonomous/merge_shadow_compare_hook.go \
  internal/autonomous/merge_shadow_compare_hook_test.go \
  internal/server/merge_shadow.go \
  cmd/datawatch/merge_shadow.go \
  internal/mcp/merge_shadow_tools.go \
  internal/router/merge_shadow_comm.go)
[ -z "$unformatted" ] || { echo "gofmt reported unformatted files: $unformatted" >&2; exit 1; }

go build ./...
go vet ./internal/mergeshadow/ ./internal/autonomous/ ./internal/server/ ./cmd/datawatch ./internal/mcp ./internal/router

# Behavior: storage round-trips and the hook tests are the check (throwaway
# data dirs and fakes).
go test ./internal/mergeshadow/ -count=1 -run "Comparison|Store|PlanMissing"
go test ./internal/autonomous/ -count=1 -run "MergeShadow"
go test ./internal/server/ -count=1 -run "MergeShadow"
go test ./cmd/datawatch/ -count=1 -run "MergeShadow"

# The old "store unused" claim is gone from the store comment.
! grep -q "no production code path calls it as of this writing" internal/mergeshadow/compare_store.go

# The PWA card + all five locale bundles carry the missing-plan key.
node --check internal/server/web/app-merge-shadow-card.js
node --test internal/server/web/app-merge-shadow-card.test.js
node -e "
const fs = require(\"fs\");
for (const lang of [\"en\", \"de\", \"es\", \"fr\", \"ja\"]) {
  const b = JSON.parse(fs.readFileSync(\"internal/server/web/locales/\" + lang + \".json\", \"utf8\"));
  if (b.merge_shadow_plan_missing === undefined) {
    console.error(lang + \": missing merge_shadow_plan_missing key\");
    process.exit(1);
  }
}
console.log(\"merge_shadow_plan_missing present in all five locale bundles\");
"
'
echo "merge shadow comparison store + missing-plan reasons: verification passed"
