# Release version note — Remove the PWA peer-stale badge from the Settings nav button and replace it with a real connectivity alert. Operator: 'R'

- Record type: **operator note** (template `version_bump_left_to_merge`)
- Recorded: 2026-11-10
- Scope: "Remove the PWA peer-stale badge from the Settings nav button and replace it with a real connectivity alert. Operator: 'R'"

## The note

For the operator's records only: the release version of "Remove the PWA
peer-stale badge from the Settings nav button and replace it with a real
connectivity alert. Operator: 'R'" is **the next patch above `main`, resolved at
merge time**. It is **never a fixed number written into the tree**.

Concretely:

1. This branch leaves the version fields **exactly as they are**:
   - `cmd/datawatch/main.go`: `var Version = "9.90.2"` (unchanged)
   - `internal/server/api.go`: `var Version = "9.90.2"` (unchanged)
   No `var Version` was touched by this task, and no `## vX.Y.Z` CHANGELOG
   heading is added on the branch by this note.
2. At merge time, `scripts/merge-branches-bump-version.sh` performs the version
   step: it chooses **the next patch above the current `main` version** (via
   `scripts/set-release-version.sh`, guarded by `scripts/check-merge-version.sh`,
   which refuses anything that is not exactly that expected next patch). Per
   AGENT.md (Versioning), feature / PRD branches do not change the version
   number; the range-based release-time check only requires the pushed range to
   end at a strictly higher version, which the final release commit provides.
3. As of the recording date, this branch's version fields stand at `9.90.2`, so
   the merge would resolve to `9.90.3` **unless `main` moves first** — and that
   number is intentionally not fixed here, because it is only meaningful at
   merge time.

This is a note, not an edit: this record writes no version number anywhere and
leaves the version fields exactly as they are.

## Verification of "note, not edit"

- `grep -n "var Version" cmd/datawatch/main.go internal/server/api.go` → both
  show `9.90.2`, identical to the branch base; no version bump was made.
- No `## vX.Y.Z` heading was added to `CHANGELOG.md` by this record.
- The only file produced by this task is this note; no version field, CHANGELOG
  heading, or release artifact was modified.

## Cross-references (same branch, same story)

- `CHANGELOG.md` entry for the peer-stale badge removal / `observer.peers.silent_alert_minutes`
  connectivity alert (the story's changelog task).
- Parity-issue draft: `docs/plans/parity-issues/2026-11-10-peer-silence-alert-parity.md`.
- Docs set: the federated-observer howto, the definitions "Federated peers" entry,
  and the `operations.md` entry for this story.

## Re-recorded — 2026-11-10

This is the third recording attempt for this story. The two earlier attempts
(their worker sessions ended in under a minute each) produced **no file at all**
and were rejected as "no changes detected". This record fixes that: the note
required by the template is now present in the working tree as this file.

Substance is unchanged from the spec: the release version of this story is still
**the next patch above `main`, resolved at merge time**, and it is still
**never a fixed number written into the tree**. Re-verified on 2026-11-10:
`cmd/datawatch/main.go` and `internal/server/api.go` both still read
`9.90.2`, untouched by this re-recording.
