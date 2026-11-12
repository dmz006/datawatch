# Stop the finish-time uncommitted work check from holding every Automaton for the documents the daemon itself generated

## Affected area

The daemon's uncommitted-work checks in the autonomous engine and every surface that
renders what they raise: the finish-time roll-up check (`checkWorktreeUncommittedAtFinish`,
internal/autonomous/uncommitted_work_rollup.go) that runs once before an Automaton is
reported finished and looks at the whole worktree; the per-task check
(`checkUncommittedWork`, internal/autonomous/uncommitted_work_task.go) that runs when a
task in an auto-commit Automaton finishes; and the exemption helpers this work added around
them (`dropUneditedGeneratedFiles` in internal/autonomous/uncommitted_work_generated_files.go,
built on `IsUneditedGenerated` / `SidecarRelPath` / `MatchesTrackedPath` in
internal/genartifacts). Through them, the PWA uncommitted-work card and chips, the
per-Automata banner and the hold question, and the same holds read through REST
(`GET /api/autonomous/uncommitted-work` and the uncommitted_work fields of the Autonomous
Automata and Task routes), the MCP tools, the CLI and the messaging channels. On the mobile
side this is the same parity surface as datawatch-app issue #320 (automaton
uncommitted-work parity): the app renders whatever holds the daemon raises, so it inherits
both the false holds and the fix without any app change of its own.

The documents involved are the daemon's own generated documents, written to their tracked
paths as each task ends and as the Automaton closes: the gate report under
docs/plans/reports/, the changelog draft and tracker rows under docs/plans/drafts/, the
parity draft under docs/plans/parity-issues/, and the registry row under
docs/plans/drafts/. Each one is a whole file the daemon created, written with a hidden hash
sidecar next to it (the base name with a leading dot and a trailing .genhash) that records
the body hash of what the daemon last wrote.

## Current behavior after this work

The story's core gap is closed. Before it, the roll-up check kept every untracked file it
found, so an Automaton that finished with only the daemon's own documents and their
sidecars still left in the worktree was held before completed with "files were still
uncommitted when the Automaton finished" (the default `hold` policy of
`autonomous.automation.uncommitted_work_policy`), and named as an uncommitted-work hold
everywhere the hold is shown. Now the roll-up check filters the detected worktree files
through `dropUneditedGeneratedFiles` before it raises or records anything: a file whose
`IsUneditedGenerated` check passes — the generated marker still present, no edited marker,
and the sha256 of the body still matching the recorded sidecar — no longer counts as
uncommitted work, and the hash sidecar of each such unedited document goes with it, so the
sidecar alone can no longer re-trigger the hold. A generated document a person touched —
marker removed, an edited marker present, or a body that no longer matches the recorded
hash — and its sidecar are kept exactly as before, still counted, and named in the hold.
The primitive fails closed: a missing file, a missing, unreadable or changed sidecar reads
as edited and keeps the hold rather than dropping it. An Automaton whose only dirt is its
unedited generated documents now completes and reaches merge ready; under the `warn`
policy the same documents no longer appear in the warning listing; an Automaton already
held whose hold listing contains only unedited daemon documents passes the check again when
it is re-evaluated. The `off`, `hold` and `warn` policies, the per-task scope setting, and
the hold's commit, discard and retry answers are all unchanged; the change is only which
files reach them in the roll-up.

The work is covered by tests at the roll-up level (a clean worktree, a worktree with one
real stray file, edited and hash-mismatched generated documents, and the sidecar-less and
sidecar-only cases), and `go build`, the package tests, gofmt and a race run are recorded
clean against it. The CHANGELOG entry is written as done, and the docs set — the
automaton-uncommitted-work howto, the operations entry and the definitions entry — names
the sidecar alongside the artifact, so the docs match the implemented behavior rather than
the pre-fix promise.

## Gaps this work leaves

The exemption is wired only into the finish-time roll-up check, by deliberate choice of the
story: `dropUneditedGeneratedFiles` documents that it "serves only the finish-time check and
never the per-task check". The per-task check still treats the generated documents and
their sidecars as ordinary uncommitted work when a task finishes, so an Automaton can be
held at task finish by files it itself generated and then passed the roll-up check by the
same files — the two checks in the same subsystem make different judgments about the same
dirt, and the current docs describe the exemption at the finish boundary only. Whether the
per-task check should get the same exemption, or whether the asymmetry is the intended
behavior to be documented as such, is an open question rather than a settled one.

The work also left a duplicate of the exemption logic in place: `excludeUneditedGeneratedArtefacts`
and its helper `uneditedGeneratedFile` in internal/autonomous/uncommitted_work_rollup.go are
superseded by `dropUneditedGeneratedFiles` but still sit in the file with no caller — two
coexisting definitions of "an unedited generated artifact", which will drift unless one is
removed.

On the app side nothing new has been contracted: the REST routes, the uncommitted_work
object, the question kind and the hold and warn texts all keep their current shape, and an
edited generated document appears in held[].files exactly like any other operator-touched
path. The only app consequence is behavioral — holds should no longer mention the daemon's
own untouched documents — so the app must not special-case the generated-document paths
itself and should keep rendering whatever the daemon reports, in English, in the same
places (card, chips, banner, question) that datawatch-app issue #320 already defines. That
"no app-side work needed" confirmation is still outstanding on #320.

## Suggested next step

File this as a datawatch (daemon) issue, since every unresolved item sits in the autonomous
checks and their helpers, and cross-reference datawatch-app issue #320 for the affected app
surface. In that issue, decide the per-task question first: either extend the same
unedited-generated exemption into `checkUncommittedWork`, filtering detected files with
the same helper before any hold or commit is raised and adding a per-task test that a task
whose only leftover is an unedited daemon document is not held, or — if the asymmetry is
intentional — say so in the howto so the operator can predict where the exemption applies
and where it does not. While that issue is open or done, remove
`excludeUneditedGeneratedArtefacts` and `uneditedGeneratedFile` from
uncommitted_work_rollup.go so the exemption has a single implementation, re-run the
roll-up and per-task suites so the CHANGELOG entry and the docs stay true to the behavior,
and then confirm on the app side that no REST-field, locale, or rendering work is needed —
recording that confirmation on datawatch-app issue #320 as the acceptance of this
follow-up.

## Filing record

Not yet filed. File as a datawatch daemon issue (the checks live in internal/autonomous),
cross-referencing datawatch-app issue #320 for the affected app surface; record the URL
and date here when filed. Drafted 2026-11-08, and the code-level claims above (the
exemption wired only into the roll-up via `dropUneditedGeneratedFiles`, the superseded
helpers still present in `uncommitted_work_rollup.go`, the unchanged per-task check, and
the matching howto, operations, definitions and CHANGELOG wording) were re-checked against
the current tree on 2026-11-09 and still hold.
