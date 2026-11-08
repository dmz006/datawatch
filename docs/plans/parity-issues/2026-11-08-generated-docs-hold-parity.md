# Stop the finish-time uncommitted-work check from holding every Automaton for the documents the daemon itself generated

## Affected area

The daemon's two uncommitted-work checks in the autonomous engine: the per-task check
(`checkUncommittedWork`, internal/autonomous/uncommitted_work_task.go) that runs when a
task in an auto-commit Automaton finishes, and the finish-time roll-up check
(`checkWorktreeUncommittedAtFinish`, internal/autonomous/uncommitted_work_rollup.go) that
runs once before an Automaton is reported finished and looks at the whole worktree. Every
operator surface that renders those holds is affected through them: the PWA uncommitted-work
card and chips, the per-Automata banner and the hold question, and the same holds read
through REST (`GET /api/autonomous/uncommitted-work`, the uncommitted_work fields of the
Autonomous Automata and the Autonomous Automaton Task routes), the MCP tools, the CLI and
messaging channels. On the mobile side this is the same parity surface as datawatch-app
issue #320 (automaton uncommitted-work parity): the app renders whatever holds the daemon
raises, so it inherits both the false holds and the fix.

The documents involved are the daemon's own generated documents, written to their tracked
paths as each task ends and as the Automaton closes: the gate report under
docs/plans/reports/, the changelog draft and tracker rows under docs/plans/drafts/, the
parity draft under docs/plans/parity-issues/, and the registry row under
docs/plans/drafts/. Each one is a whole file the daemon created, and it is written with a
hidden hash sidecar next to it (the same directory, the base name with a leading dot and a
trailing .genhash) that records the body hash of what the daemon last wrote.

## Current behavior after this work

The story "Stop the finish-time uncommitted work check from holding every Automaton for
the documents the daemon itself generated" landed in two pieces. First, internal/genartifacts
gained three primitives, all covered by tests: MatchesTrackedPath tells whether a
repository-relative path is the tracked path of one of the generated document kinds for
some Automaton (anchored, per kind, so the changelog draft and tracker rows that share a
directory are told apart; absolute paths, dotdot paths, and non-clean paths are rejected);
SidecarRelPath names that document's hash sidecar; and IsUneditedGenerated reports whether
the tracked file is still exactly what the daemon last wrote — first line is the generated
marker, no edited marker is present, and the sha256 of the body equals the hash in the
sidecar — and fails closed: a missing file, a directory, a missing or changed sidecar, or
any read error all report false.

Second, the CHANGELOG already carries the fix as done: an unedited generated document is
said to "no longer count as uncommitted work", holds are said to no longer name generated
documents the daemon wrote unchanged, and an Automaton already held on only generated
documents is said to pass the check again when it is re-run.

That promise does not hold yet in this tree. Neither check consults the new primitives:
there is no caller of MatchesTrackedPath, SidecarRelPath, or IsUneditedGenerated outside
internal/genartifacts and its tests. The existing dirt filter (filterDaemonOnlyPorcelain)
only drops uncommitted modifications of already-tracked files whose sole difference is the
daemon's own rule-file sections; the generated documents are brand-new untracked files, and
untracked records are always kept as dirt. So today an Automaton that finishes with only
the daemon's own documents and sidecars left in the worktree is still held before completed
with "files were still uncommitted when the Automaton finished" (default policy hold), named
as an uncommitted-work hold on the PWA card, the daemon chips, the banner and the question,
and mirrored into every app and surface that reads the REST contract. Under the warn policy
the same documents produce a warning decision instead. A person-edited generated document
holds today too, and it is the one case where holding or warning is the right behavior.

## Expected behavior

The finish-time roll-up check and the per-task check should both drop, from the files they
treat as uncommitted work, exactly the documents the daemon itself generated and left
unedited — a tracked-path document whose IsUneditedGenerated check passes — together with
that document's hash sidecar as named by SidecarRelPath. The sidecar has to be dropped
alongside the document because it is an untracked file in the same worktree and nothing in
.gitignore hides it: if only the document were excluded, its sidecar alone would re-trigger
the hold and the fix would not be visible. A generated document that a person touched — the
marker removed, an edited marker present, or a body that no longer matches the recorded
hash — stays in the dirt exactly as it is now: it counts toward the hold, names the same
hold, and shows in the same places. Every other uncommitted file is treated exactly as
before, and the fail-closed behavior of the primitive (any doubt reads as edited) means an
unreadable sidecar or file keeps the hold rather than dropping it. The hold and warn
policies, the task_files and worktree scopes, and the policy's answer and commit actions
are all unchanged; the change is only which files reach them. Once the check is fixed, an
Automaton already held whose hold listing contains only unedited daemon-generated documents
passes the check again when it is re-evaluated, matching the CHANGELOG entry.

For the mobile apps this is a daemon-side exclusion and no new contract: the REST routes,
the uncommitted_work object, the question kind, and the hold and warn texts all keep their
current shape, and an edited generated document appears in held[].files exactly like any
other path the operator touched. The only parity consequence for the app is behavioral —
holds should no longer mention the daemon's own untouched documents, so the app must not
special-case the generated-documents paths itself and should keep rendering whatever the
daemon reports, in English, in the same places (card, chips, banner, question) that issue
#320 already defines.

## Suggested next step

Wire the primitives into the two checks: in the per-task detect path and in
checkWorktreeUncommittedAtFinish, filter the detected uncommitted files through
IsUneditedGenerated (with the task's project root) before any hold, warn, decision, or
commit is raised, dropping each passing document and its SidecarRelPath alongside it, and
leave everything else untouched. Add a roll-up-level test that an Automaton worktree whose
only dirt is unedited generated documents and their sidecars reaches completed, one whose
dirt includes a person-edited generated document is still held (or warned under the warn
policy) and names that file, and one whose sidecar is the only leftover is not held.
Re-run the per-task and roll-up suites against the CHANGELOG entry's wording so the entry
stays true, and then confirm on the app side that no REST-field, locale, or rendering work
is needed — recording that confirmation on datawatch-app issue #320 as the acceptance for
this follow-up. File this as a datawatch (daemon) issue, since the gap is entirely in the
autonomous checks; the app has no work to do beyond continuing to render the daemon's
holds.

## Filing record

Not yet filed. File as a datawatch daemon issue (the checks live in internal/autonomous),
cross-referencing datawatch-app issue #320 for the affected app surface; record the URL and
date here when filed.
