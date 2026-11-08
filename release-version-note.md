# Release version note (operator's records)

Recorded: 2026-11-03 (session `td9a0004`, template `version_bump_left_to_merge`)

## Note

The release version of the work item:

> Fix the built-in task templates so a decomposed Automaton's final gate and
> version note tasks pass the verifier instead

is **the next patch above main, resolved at merge time**.

It is **never a fixed number written into the tree.** This note records that
fact for the operator's records only. It is a note, not an edit: it writes no
version field, changes no version in `cmd/datawatch/main.go`,
`internal/server/api.go`, or `CHANGELOG.md`, and leaves all version fields
exactly as they are.

## Basis

Per this repository's AGENT.md (Versioning → "Feature branches and merges"):

- Feature branches do not change the version number.
- `scripts/merge-branches-bump-version.sh` performs the version step when
  branches reach main: it chooses the next patch above the current main
  version and records it in one bump commit covering every branch merged in
  that run.
- `scripts/check-merge-version.sh` runs before each branch is merged and
  refuses a branch whose version change is not the expected next patch.
- The merge helper does the bump via `scripts/set-release-version.sh`; the
  version should not be written by hand on a branch.
