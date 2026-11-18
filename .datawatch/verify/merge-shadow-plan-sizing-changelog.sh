#!/usr/bin/env bash
# Verification: the shadow merge plan derives its release size from the
# Automaton's kind (feature → minor, bug → patch) or its own release-size
# setting, its planned version is the next version after the current version
# on the base ref, and the changelog comes from the Automaton's own draft
# file under docs/plans/drafts first, then its entry-task output, with the
# source used recorded.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

env -u TMUX -u TMUX_TMPDIR bash -c '
set -euo pipefail

unformatted=$(gofmt -l \
  internal/mergeshadow/runner.go \
  internal/mergeshadow/changelog.go \
  internal/mergeshadow/runner_sizing_test.go \
  internal/mergeshadow/changelog_draft_test.go \
  internal/autonomous/merge_shadow_hook.go)
[ -z "$unformatted" ] || { echo "gofmt reported unformatted files: $unformatted" >&2; exit 1; }

go build ./...
go vet ./internal/mergeshadow/ ./internal/autonomous/

# Behavior: the sizing + changelog-source suites themselves are the check
# (throwaway repos and data dirs, fakes — no real git beyond the throwaways).
go test ./internal/mergeshadow/ -count=1 -run "Runner|Sizing|ReleaseSize|Changelog"
go test ./internal/autonomous/ -count=1 -run "MergeShadow|ChangelogKind"

# The recorded draft-file source must be the fixed string, in code and in the
# howto.
grep -q "\"changelog_draft\"" internal/mergeshadow/changelog.go
'
echo "merge shadow plan sizing + changelog sources: verification passed"
