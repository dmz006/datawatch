# Release version note — Part of finishing review and merge by the daemon (Phase 4 of the autonomy handover plan)

- Record type: **operator note** (template `version_bump_left_to_merge`)
- Recorded: 2026-11-07
- Scope: "Part of finishing review and merge by the daemon (Phase 4 of the autonomy handover plan). Operator, 2026-11-07, quoted:"

## The note

The release version of this slice is **the next patch above `main`, resolved at merge time**.
It is **never a fixed number written into the tree**.

Concretely:

1. This branch leaves the version fields **exactly as they are**:
   - `cmd/datawatch/main.go`: `var Version = "9.112.3"` (unchanged)
   - `internal/server/api.go`: `var Version = "9.112.3"` (unchanged)
   No `var Version` was touched by this task, and no `## vX.Y.Z` CHANGELOG heading is added
   on the branch.
2. At merge time, `scripts/merge-branches-bump-version.sh` performs the version step: it
   chooses the **next patch above the current `main` version** and records that version in one
   bump commit covering every branch merged in that run. Per AGENT.md, feature branches do not
   change the version number; the merge is strictly higher than the range start, so the
   range-based release-time version check passes without per-task bumps.
3. As of the recording date, `main` stands at `9.112.4`, so the merge would resolve to
   `9.112.5` **unless `main` moves first** — and that number is intentionally not fixed here,
   because it is only meaningful at merge time.

## Verification of "note, not edit"

- `grep -n "var Version" cmd/datawatch/main.go internal/server/api.go` → both show `9.112.3`,
  identical to the branch base; no version bump was made.
- The only file produced by this task is this note; no version field, CHANGELOG heading, or
  release artifact was modified.

## Re-record — 2026-11-08

The same operator note is re-recorded here on 2026-11-08 for the task record of this slice
(automaton task `cd500005`, template `version_bump_left_to_merge`), replacing the earlier
2026-11-08 attempt that produced no file at all. Nothing about the note changed: the release
version of this slice is still **the next patch above `main`, resolved at merge time**, and it
is still **never a fixed number written into the tree**.

Re-verified on 2026-11-08:

- `grep -n "var Version" cmd/datawatch/main.go internal/server/api.go` → both still show
  `9.112.3` (branch base, untouched); no version bump was made by this re-recording.
- The only change produced by the 2026-11-08 task is this re-record section; no version
  field, CHANGELOG heading, or release artifact was modified.
- The merge-time mechanics stand as recorded above: `scripts/merge-branches-bump-version.sh`
  chooses the next patch above the `main` version as it stands when the merge runs, and
  `scripts/check-merge-version.sh` refuses anything that is not that expected next patch.
  Whatever `main` is at that moment, that number is what this slice ships as — and it is not
  fixed here.

## Parity surface

This note changes no PWA, app or other client surface: it records how the release version is chosen at merge time and adds no setting, endpoint, message or screen. The Automaton it belongs to (99be0001) does change the Merge pipeline card in the PWA and the merge status read; those drafts are in `docs/plans/parity-issues/` (see `2026-11-08-daemon-merge-run-parity.md` and `2026-11-08-release-bump-validator-parity.md`).
