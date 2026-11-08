# Release version note — Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier instead

**Recorded:** 2026-11-08 · **Kind:** operator record — a note, not an edit · **Carries no version number of its own**

## The note

For the operator's records only: the release version of "Fix the built-in task
templates so a decomposed Automaton's final gate and version note tasks pass the
verifier instead" is **the next patch above main, resolved at merge time**. It is
never a fixed number written into the tree. This is a note, not an edit: it never
writes a version number and leaves the version fields exactly as they are.

This record deliberately carries no version number of its own (matching the
precedent in `docs/plans/parity-issues/header-tags-icon-filled.md` — "this record
deliberately carries no version number"). The concrete number exists only when it
does, at merge time.

## Why the number is resolved at merge time, not here

Per AGENT.md (Versioning → "Feature branches and merges"):

- A feature / PRD branch does **not** change the version number. Neither
  `cmd/datawatch/main.go` nor `internal/server/api.go` is edited on the branch, and
  no `## vX.Y.Z` heading is added to `CHANGELOG.md` on the branch.
- `scripts/merge-branches-bump-version.sh` chooses **the next patch above the
  current main version** when the branch reaches main, through
  `scripts/set-release-version.sh`. `scripts/check-merge-version.sh` guards each
  merge and refuses a version change that is not exactly that expected next patch.
- The release-time version check is range-based: the individual task commits in
  this story do not each bump the version — only the final release commit must land
  the pushed range at a strictly higher version than it started.

So the release number is a function of wherever main's head is at merge time, not a
value recorded in the tree.

## State of the version fields (left exactly as they are)

| File | Change made by this note |
|------|--------------------------|
| `cmd/datawatch/main.go`  | none — version field untouched |
| `internal/server/api.go` | none — version field untouched |
| `CHANGELOG.md`           | no `## vX.Y.Z` heading added by this note |

No version bump, no CHANGELOG heading, and no version number is written into any
file by this record.

## Cross-references (same branch, same story)

- `CHANGELOG.md` entry (the story's changelog task).
- Docs set: the howto/operations/definitions entries for the template fix.
- Parity-issue draft: `docs/plans/parity-issues/2026-11-08-built-in-task-templates-verification.md`
  (which describes this version note as "a plain note-kind, no-files record ... the
  verifier has nothing to run against it and nothing to hold for it").

## Re-recorded — 2026-11-08 14:07

Recorded again for the operator's records, unchanged in substance: the release
version of "Fix the built-in task templates so a decomposed Automaton's final gate
and version note tasks pass the verifier instead" is **the next patch above main,
resolved at merge time** by `scripts/merge-branches-bump-version.sh` (via
`scripts/set-release-version.sh`, guarded by `scripts/check-merge-version.sh`).
It is never a fixed number written into the tree.

Still a note, not an edit: this re-recording adds no version number anywhere and
leaves the version fields exactly as they are — `cmd/datawatch/main.go` and
`internal/server/api.go` are untouched, and no `## vX.Y.Z` heading is added to
`CHANGELOG.md`.

(The original note from this story is already in this branch's history; this entry
re-records it so the requirement is present in the working tree again.)
