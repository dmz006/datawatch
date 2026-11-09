# Parity draft — the shadow merge comparison: grades the shadow plan against what actually landed, and keeps each difference as a defect candidate (no datawatch-app surface exists yet)

## Not yet filed, and not yet fileable

This step closes nothing on the app side. It builds the daemon's own evidence
machine for Phase 4 of the autonomy handover: after an Automaton's merge has
landed and its shipped record is written, the daemon reads what the default
branch's ref actually holds, compares it field by field with the stored shadow
merge plan, grades every disagreement (major or minor), and keeps each
disagreement as a defect candidate under the daemon's data directory. None of
that is visible to an operator through REST, MCP, CLI, alerts, messaging, or
the PWA, and nothing here is filed into a defect queue — filing is a separate
job of the existing defect intake. The step also leaves three known internal
gaps (the parity-issue field compares against the plan's own value, gates count
as not compared, and the full comparison record is not persisted), none of
which is an operator-facing defect either. Record the intended operator-facing
surface below so a datawatch-app parity issue can be filed the moment one of
those surfaces actually lands, rather than now, against a contract that does
not exist yet.

## Affected area

The step is daemon-side only: the landed-state reader (`ReadLanded` in
`internal/mergeshadow/landed.go`), the grader (`Compare` in
`internal/mergeshadow/compare.go`), the comparison store
(`SaveComparison`/`LoadComparison`/`ListComparisons` in
`internal/mergeshadow/compare_store.go`), the defect-candidate store
(`DefectCandidatesFromComparison`/`RecordComparisonCandidates`/
`ListDefectCandidates`/`ListAllDefectCandidates` in
`internal/mergeshadow/defect_candidate.go`), and the post-ship hook
(`startMergeShadowCompare` in `internal/autonomous/merge_shadow_compare_hook.go`)
that runs all of it in a background goroutine after `RecordAutomationShipped`.
Tests cover each piece (`landed_test.go`, `compare_test.go`,
`defect_candidate_test.go`, `merge_shadow_compare_hook_test.go`), and the docs
set already names the behavior: the "How the shadow comparison runs" and "What
is still yours to do" sections of `docs/howto/merge-pipeline.md`, the shadow
merge plan entry in `docs/datawatch-definitions.md`, the shadow-merge-plan
entry in `docs/operations.md`, and the CHANGELOG entry describing the defect
candidates.

When the intended surface lands, the operator-facing area in datawatch-app is:
a defect-queue read that shows a candidate's field, planned value, landed value,
severity and release size (plus the existing defect-intake surface it will be
filed into); a comparison read showing the plan-versus-landed state for one
Automaton with its matched/different/not-compared counts and its differences;
and the alert or question text a major difference raises when the phase moves
out of shadow. Today none of these exist in the daemon either — the plan, the
comparison and the candidates have no REST route, MCP tool, CLI verb or PWA
card — so the app has nothing to render yet. The app-side consequence is the
same kind as the sibling drafts for this same phase ([shadow
merge plan](shadow-merge-plan-parity.md) and [merge
pipeline](merge-pipeline.md)): the datawatch-app is a thin client that renders
whatever the daemon reports, so when any of these surfaces exists the app must
render it in English without inventing its own grading, and the daemon's REST
contract (field names, the `major`/`minor` vocabulary, the `(missing)` marker,
the `candidate` status) becomes app-visible and needs the usual locale pairing
and the "what to do once one exists" walkthrough in the howto.

## Current behavior after this work

The story's core gap — the daemon has no way to judge, after the fact, whether
the merge it planned in shadow would have matched the merge that actually
landed — is closed at the daemon level. The sequence is:

- **The ship path never waits on it.** When the shipped record is written for
  an Automaton, the hook starts the comparison in a background goroutine under
  a two-minute budget; an expired budget, an unresolvable base ref, or any
  other failure is logged and dropped. The ship already happened and nothing
  about it changes, an Automaton whose shipped record was just written is never
  re-held or re-alerted, and an Automaton with no stored plan is a no-op with
  no git read at all (the hook does not invent a plan).
- **It reads from the ref, never the tree.** The landed state is read from the
  plan's base ref, or `main` when the plan names none, through `git show
  <ref>:<path>` only: the first `## ` heading of `CHANGELOG.md`, the `var
  Version` line of every version file the pipeline names (a file missing on
  the ref, or missing the line, reads as not found and appears as `(missing)`
  in the version side of a difference), the `Current release` line of
  `README.md` matching the planned version, and the registry table row naming
  the Automaton from the pipeline's post-ship registry file with its
  `{project}` placeholder filled in, or `docs/plans/README.md` when the
  pipeline names none. Uncommitted edits in the working tree and checkouts of
  other branches cannot change the result; a ref that does not resolve to a
  commit is an error.
- **It grades, not just diffs.** Comparing the plan and the landed state is
  field by field — version (every version file must carry the planned value;
  the landed side lists each file's value), changelog heading, README line,
  registry row, parity issue, and gates. A difference is **major** when a
  version is wrong or missing, a planned heading, README line or registry row
  landed nowhere, a parity issue the plan required landed nowhere, or a planned
  gate is absent from the supervisor's manual gate record or present with a
  different result (the gate field is only checked when that record is
  supplied); **minor** when the heading, README line or registry row drifted in
  wording, or a parity issue landed where the plan did not require one. A
  missing landed value is a difference with the `(missing)` marker, not a skip.
- **Each difference leaves a defect candidate; the full comparison is not
  persisted yet.** Every difference becomes a candidate with a stable content-derived
  key (Automaton id, field, planned and landed values, release size, status
  and creation time), status `candidate`, stored idempotently by content under
  `<data dir>/defect-candidates/<id>.json` (one file per Automaton): running
  the comparison again on unchanged data adds nothing and the stored candidate
  keeps its original key and creation time, while a candidate whose planned or
  landed value did change is new. A full comparison record, one JSON per
  Automaton under `<data dir>/merge-comparisons/` with its matched, different
  and not-compared counts, exists in the code but the post-ship flow does not
  call it yet — only the differences are persisted, not the whole
  comparison.
- **What it does not do.** It does not file the candidate into the defect
  queue — that is the existing defect intake's job — does not alert, does not
  raise a question or hold, does not create, move or delete a branch, tag or
  ref, does not push, and never applies the merge. Its only write is the
  candidate file. The supervisor's manual merge stays the source of truth on
  every side.

Three known internal limitations sit in the current behavior and should be
carried into the daemon issue when one is filed (they are daemon gaps, not app
gaps): the parity-issue field currently compares the plan against its own
`parity_issue_needed` value because the hook has no reader for the landed
presence, so that field always matches and can never raise a difference; the
gate field counts as not compared because the hook has no source for the
supervisor's manual gate record (the grader would check it if one were
supplied); and the comparison itself is computed in memory but never persisted
beyond its candidates, so the matched/different/not-compared counts a
comparison produced are lost after the run.

The docs are already honest about all of this: the howto names the
`merge-comparisons/` store as existing in the code but not being called, the
gate record as "only checked when that record is supplied", the parity-issue
field as landing "where the plan did not require one" (minor) vs "nowhere"
(major), and "what you still review" includes the parity-issue and gate lines
that have no landed side yet.

## Expected behavior when the gap closes

The moment any part of this mechanism becomes operator-visible, the
datawatch-app surface should read as a consistent story, in English, with the
daemon as the single source of truth for every fact:

- **Defect-queue read.** When candidates are filed into the defect queue (or
  listed through it), whatever the queue surface is it should show every
  candidate with its field, planned value, landed value, severity, release size
  and status, and a comparison with no differences should show as empty rather
  than broken, and re-filing an unchanged candidate should not double it (the
  daemon store is idempotent by content, and the queue should be too).
- **Comparison read.** Once the comparison is stored and exposed (route, tool,
  verb or card), the app should show the plan-versus-landed state for one
  Automaton with its matched, different and not-compared counts and its
  differences, with the `(missing)` marker rendered plainly and the
  `major`/`minor` vocabulary used as-is. A gate field that the supervisor
  has not recorded should show as not-compared rather than as a defect.
- **Alerts.** When a major difference raises an alert or question, or when the
  phase's mode moves out of shadow, the text should name the Automaton, the
  field, the planned and landed values, and the severity, and the app should
  surface the same alert in the channels it already surfaces daemon alerts
  through, without inventing its own verdict.
- **Docs.** The howto's "What is still yours to do" section should be updated
  so that, when a surface exists, it names it instead of saying "no operator-
  facing reads yet", and the parity drafts (this one and its siblings for the
  plan and the pipeline) should be updated to point at the filed datawatch-app
  issue for the affected app surface, the way the sibling drafts do.

## Suggested next step

Do not file a datawatch-app issue yet. None of the intended surfaces — the
defect-queue listing of a candidate, a comparison read, or a major-difference
alert — exists in the daemon, and there is no REST route, MCP tool, CLI verb or
PWA card for the plan, the comparison or the candidates to contract against,
so an app issue now would be a list of shapes that do not exist. Re-check this
draft when the daemon exposes any of these reads (or when the candidates are
filed into the existing defect queue), and then file a datawatch-app parity
issue that names the REST/MCP shape, the severity and status vocabulary, the
`(missing)` marker, and the locale pairing, cross-linking it back to this
draft, the sibling [shadow merge plan draft](shadow-merge-plan-parity.md) and
the [merge pipeline draft](merge-pipeline.md), and noting in the filing record
here that the app issue is filed and where. Separately, and in the daemon
repo, the three internal limitations (no parity-issue presence reader, no
source for the supervisor's manual gate record, and the comparison itself not
persisted beyond its candidates) should be tracked so the evidence bar Phase 4
defines (ten merges where the daemon's plan equals the manual one or is better,
including at least two with conflicts and two with PWA changes, no blocking
comparison defect) can actually be evaluated against a complete comparison
record when the time comes.

## Filing record

Not yet filed, and not yet fileable. Filed the moment any of the intended
surfaces (defect-queue read, comparison read, major-difference alert) exists in
the daemon, and never on the app side alone: the datawatch-app issue, once
filed, names the daemon's REST/MCP contract for the affected reads and cross-
links the daemon issue and the sibling drafts, and this draft's filing record
updates to point at it. Drafted 2026-11-09, from the worktree of Automaton
`d2f10001` (Phase 4, the shadow merge and comparison with the replay tests),
and the code-level claims above (the background hook under a two-minute budget
after `RecordAutomationShipped`, the `git show <ref>:<path>`-only landed read,
the major/minor grading with the `(missing)` marker, the idempotent candidate
store under `defect-candidates/`, the `merge-comparisons/` store existing but
not being called by the post-ship flow, the parity-issue field comparing
against the plan's own value, the gate field counting as not compared, and the
absence of any REST route, MCP tool, CLI verb or PWA card for the plan, the
comparison or the candidates) were checked against the current tree on
2026-11-09 and still hold. Its siblings: [shadow merge
plan](shadow-merge-plan-parity.md) (the plan step this grades) and [merge
pipeline](merge-pipeline.md) (the operator-visible pipeline this lives under).
