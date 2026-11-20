# Release plans and supervisor status

**Date:** 2026-11-19 (evening)
**Version:** none (status document; plan-document commits are exempt from the version bump)
**Replaces:** the 2026-11-15 plan listing and the 2026-11-19 morning open-work listing, both kept only in the supervisor scratch folder and now out of date.

Read this page for where the build stands: the two release plans, what shipped today, what is open and why, how the supervisor itself is run, and what is being done to make that cheaper. Titles lead; the short record id follows in brackets so a row can be found on the Automata page.

## Release plans

| Plan | State | Progress | Next up |
|---|---|---|---|
| Release 10.0 | in_progress | 289 of 406 done (71%) | {'automaton_id': 'a7f20001', 'title': 'Fix open high and critical CodeQL alerts'} |
| Release 11.0 | planned | 3 of 24 done (12%) | {'automaton_id': '14310001', 'title': 'Stale browser state cannot break the Sessions view-mode buttons'} |

Release 10.0 members by state: archived 314, completed 25, needs_review 22, draft 16, blocked 10, failed 5, cancelled 5, approved 5, running 3, revisions_asked 1.

Reading the progress figure honestly: on 2026-11-15 the plan read 286 of 318 done (89%). Today it reads 289 of 406 (71%). The total grew by 88 because new work (defects found while building, follow-ups, backfilled records) was attached to the plan, while the done count moved by only 3. The percentage fell because the plan grew, not because finished work was lost. The 25 completed records are finished and waiting to be archived; some cannot be archived yet (see the supervisor loop section).

## What shipped today

Every item below is a patch release made from a merged Automaton. No minor version was tagged; the last official tag is still the one from 2026-11-14, with nine minors (9.174 to 9.182) and the patches since not yet tagged. When to cut a minor is the operator's decision (a feature is done only when all its pieces are).

| Release | What it did |
|---|---|
| 9.182.39 | Pre-flight check no longer trips on a shell `||` with an empty side (stopped a daemon crash loop of ten aborts on 2026-11-18) |
| 9.182.40 | Decompose output abort fix and crash diagnostics |
| 9.182.41 | Repair of the Automaton cleanup passes: reaper wiring, skip resolution, remote reap, temp sweep |
| 9.182.42 | A finished task no longer retries its merge forever when its branch is missing or untracked files block the merge (the daemon recreates the branch from the task worktree and moves differing files aside, never deleting) |
| 9.182.43 | Security: the GitHub CLI in the agent-base image is rebuilt with a Go release that fixes a HIGH standard-library vulnerability; the Go pin floor check rejects the vulnerable releases |
| 9.182.44 | Removed the CPU burner and fixed intermittent tests at their root |
| 9.182.45 | The archive guard covers every terminal state |
| 9.182.46 | Scan fix loop settings work and are documented |
| 9.182.47 | Orphaned pi-pool slots held by judges and verifiers are freed by a periodic reconcile |
| 9.182.48 | The pull report never shows an attempt without its outcome |
| 9.182.49 | Planning Automata stay in planning across a daemon restart |

The 9.182.43 image fix was merged with the local image scan overridden (the 2026-11-14 operator decision, because the scanner cannot read layers saved by the container engine in use). Its real check is the next scheduled image refresh in CI, which had not run after the fix when this was written.

## Open work in Release 10.0

### Running now (3)

| Priority | Work | Record |
|---|---|---|
| 90 | Live restart check and records for keeping planning across a restart | 73630001 |
| 80 | Repair three failing PWA end-to-end stories | 2acf0001 |
| 80 | Guard no-PWA specs and smoke-test the priority-stalls surfaces | 9c8f0001 |

### Approved, waiting to start (5)

| Priority | Work | Record |
|---|---|---|
| 80 | Scan fix loop route contract, docs and parity coverage | fc920001 |
| 80 | Guard script rename and shutdown-cause stop-match check | ecc10001 |
| 80 | Fix docs_list_howtos returning no how-tos on a sandbox daemon | a4280001 |
| 80 | Fix web UI load failure in the sandbox daemon and fail fast in the runner | 21130001 |
| 80 | Mobile-fit nav icon clipping and server dropdown tap target | 9cdb0001 |

### Waiting for approval (decomposed) (22)

| Priority | Work | Record |
|---|---|---|
| 90 | Keep planning Automata in planning across a daemon restart | 04b60001 |
| 70 | Release leaked capacity slots, sweep them, and show slots and lease state | af960001 |
| 60 | Supervisor watch worktree check uses the Automaton's own project | e0770001 |
| 20 | Plan-coverage unresolved-file extraction fix | 234e0001 |
| 20 | Working llm models add, remove and list on every surface | 10980001 |
| 20 | Launcher answers the development-channels confirmation and verifies the task text | cfb30001 |
| 20 | Schedule CLI goes through the daemon | 6acd0001 |
| 20 | The schedule list shows fires, last fire time and result for schedules that fire | 674d0001 |
| 20 | Whole-title changelog entries | f6430001 |
| 20 | Memory-bounded merge test runs | db6f0001 |
| 20 | Gate runner ignores launcher files and worktrees generate the docs-index embed asset | 440c0001 |
| 20 | Loaded-model retry on placement refusal | 52b40001 |
| 20 | Merged Automata reach Completed whatever their review state | 96180001 |
| 20 | Close the remaining CodeQL high and critical alerts | 0be70001 |
| 20 | Scan settings writes take effect, persist, and refuse what they cannot apply | 44230001 |
| 20 | Untracked verifier inputs are committed or named at task start | 0d0e0001 |
| 20 | Completion landing setting autonomous.completion_landing.enabled on every config surface | 2c5d0001 |
| 20 | Document priority_stalls in the Cursor MCP tools table and guard the table | 4ae00001 |
| 20 | Real evidence in the generated gate report | 04a00001 |
| 20 | Judge timeout hold replay, honest review state and large-diff judging | e3420001 |
| 20 | Decompose lint catches generated gate script faults | 88980001 |
| 20 | Confine the session-end reap to descendants of the session | 45f10001 |

### Spec revised, waiting for a fresh plan (1)

| Priority | Work | Record |
|---|---|---|
| 85 | Supervisor session start and managed restart | 3b270001 |

### Drafts (not decomposed) (16)

| Priority | Work | Record |
|---|---|---|
| 85 | (untitled record) | e1940001 |
| 80 | (untitled record) | 38090001 |
| 70 | (untitled record) | 09c30001 |
| 70 | (untitled record) | c8f50001 |
| 65 | (untitled record) | 13cf0001 |
| 60 | (untitled record) | 08170001 |
| 60 | (untitled record) | 32100001 |
| 60 | (untitled record) | 94560001 |
| 55 | (untitled record) | e81e0001 |
| 50 | (untitled record) | 526f0001 |
| 40 | (untitled record) | 8a3e0001 |
| 40 | (untitled record) | 64650001 |
| 20 | (untitled record) | 70ae0001 |
| 20 | (untitled record) | 929d0001 |
| 20 | (untitled record) | f3540001 |
| 20 | (untitled record) | 3c680001 |

### Blocked (10)

| Priority | Work | Record |
|---|---|---|
| 90 | Fix the shadows: plan size and version, changelog source, stored comparison, missing-plan reason | f31d0001 |
| 90 | Residue cleanup after Automaton merge | 18990001 |
| 80 | Make the internal/autonomous race-detector run pass | d5060001 |
| 80 | Fix eBPF generated-code drift with pinned toolchain | e3060001 |
| 80 | Lean sessions list datawatch tools and richer changelog draft | 60440001 |
| 20 | Move Automata configuration into Settings and replace the usage calculator with small dashboards | 9f8f0001 |
| 20 | Merge scripts in the repository with shadow pipeline comparison | 71f80001 |
| 20 | (untitled record) | 2fa30001 |
| 20 | Configuration parity audit and CI gate | 70900001 |
| - | BL433 — PWA performance audit under load (design/audit pass) | 049f4713 |

### Failed (5)

| Priority | Work | Record |
|---|---|---|
| 90 | Remove the remote reap PWA part and document its settings | c07b0001 |
| 90 | (untitled record) | 27720001 |
| 80 | Plans migration: history rows, real titles, explained conflicts, full counts | bcf30001 |
| 80 | Scope the supervisor watch worktree check to datawatch worktrees | a92a0001 |
| 80 | Make the full release smoke pass on main | 87620001 |

## Release 11.0

Planned, 3 of 24 done. Members by state: archived 8, needs_review 6, blocked 5, draft 4, cancelled 1. The blocked ones are the model evaluation runs and designs, parked until after the 10.0 release by the operator.

## What went wrong today and where it stands

| Problem | Cause found | State |
|---|---|---|
| Finished tasks retried their merge for ever | Branch missing, or untracked files in the way | Fixed in the daemon (9.182.42), verified live on two Automata |
| Several Automata point at the main checkout | Created by defect intake with the repository's own folder as the project directory; workers then edit the main checkout (one earlier failure left edits there) | Four recreated without a project directory. The intake fix is the Automaton "Keep Automata off the main checkout" (54a20001, running). Six others were approved against the main checkout at 18:27 by another actor; one of them has already written staged changes into the main checkout, which blocks the next merge until it is committed or the Automaton is recreated. Needs the operator's call |
| A task held for review after three failed attempts (archived-unmerged MCP docs) | Its gate script sources helper scripts that exist only in the Automaton's worktree, because the hidden verifier folder is not tracked and the task worktree was made earlier | Daemon retried it itself. Root cause is the Automaton "Untracked verifier inputs are committed or named at task start" (0d0e0001, still waiting) |
| A pi task failed with "model not loaded" | The task asked for a model name the node does not serve; the node is pinned to the 64k variant | Task reset onto the loaded model. The general fix is "Loaded-model retry on placement refusal" (52b40001, waiting) |
| Judge retried a 2,300-line prompt every 30 minutes on the pi judge | Large rules-review prompt on the small local judge; the third try was routed to Claude and finished | Class covered by "Judge timeout hold replay, honest review state, large-diff judging" (e3420001, waiting) |
| The usage guard showed the wrong window figure | I used four window percent per week point; the guard file's own rule says 9.2 and the latest reading implies about 14 | Corrected after the operator's 18% window and 65% week reading. The week estimate was close (65.8 against 65). No action had been wrong, every figure was far under the 85% cutoff |
| Week estimate crossed the pace line | The estimate rested on a reading two days old | New Claude work held until 21:00 (back-off); lifted by the operator's reading showing only about +0.5 |
| The daemon's archive step errors | `close_finished_records` refuses to archive Automata whose branch it does not see as landed on the default branch (eight named in the last runs), including ones merged by the supervisor's merge helper | Open: the landing record for merges done outside the daemon is not written. Not yet an Automaton |

Other standing items: the CodeQL workflow is red on every recent main commit and was not rerun (security workflows are never rerun by the supervisor); the daemon's `AGENT.md` in the main checkout is repeatedly rewritten with an injected block while a long-running supervisor session works in that folder, and is restored by `git checkout` each time.

## How the supervisor is run, and the plan to change it

**Today.** Three things run, all outside the product's own scheduling:

- a cheap script, the watch (every 20 minutes), that prints one line when all is well and a findings list otherwise; a model only reads it when there is something to fix;
- a window guard (every 10 minutes) that computes the usage estimates and writes one log line;
- long-running Claude supervisor sessions that do the actual work on findings (merge, deploy, feed lanes, triage). Two were alive at the time of writing. They keep their context between ticks.

A long session is the costly part. The design notes measure one supervisor starting near 59 thousand tokens and growing to about 380 thousand after roughly 700 turns, after which it is no longer lean and forgets standing rules. In the 16:00 window the supervisor sessions made up about 0.7 of the 1.2 week points burned (58%), the workers the rest.

**The daemon's own supervisor loop (built, running).** 11 jobs, 10 enabled: seven mechanical jobs (close finished records, deploy after a ship, retry infrastructure failures, cleanup sweep, apply answers, lane count, start next chain step), one mechanical job switched off (run the merge until a conflict), and three model jobs that run one-shot (draft a spec, classify a CI failure, pick a backend). Each model job has a selectable style, backend and model, and every run writes a record (read with the supervisor-loop jobs, stats and runs commands), which is the operator's 2026-11-01 requirement. 500 runs are stored: 429 ok, 71 error, none escalated, none retried. The errors seen are the archive refusals in the table above.

**Run the supervisor fresh every session (not started).** The operator wrote this feature as the Automaton "Supervisor session start and managed restart" (3b270001). It asks for: a supervisor session assembled from three layers (a lean build context; the supervisor rules as an installable rules pack read from the project; and the direction read from the project's own plans); a new short session for every tick and every trigger event, which reads the layers and a short state record held in daemon memory, acts, writes the record and ends; corrections from the operator or shadow recorded with a class (one-off state, standing rule promoted into the pack, or product defect filed as an Automaton) and a repeat detector; one idempotent command to set a project up; and a fallback that restarts a long session when it passes a size or age limit. Status: the operator revised the spec four times on 2026-11-18 (last edit 21:27). It was planned into 18 tasks in two stories before those edits, so it needs a fresh plan, then approval. I reported it as waiting on the operator for a day; it is the supervisor's step, held only because planning is Claude work and Claude is backed off until 21:00. Next step: re-plan with the usual guidance block, check coverage and approve.

**Optimisations worth the operator's decision** (none changed without it):

1. Land the fresh-spawn feature, then run ticks on Haiku as one-shot sessions with the state record in memory.
2. Until then, cut the guard to every 20 minutes and fold it into the watch; most guard ticks change nothing.
3. Keep only one long supervisor session alive (a second one was found running; the approvals at 18:27 appear to come from it, which was not confirmed).
4. Write the landing record when the supervisor's own merge helper merges, so the daemon can archive those Automata and the loop's errors stop.
5. Get a usage reading from the operator every couple of hours, and fix the window rule in the guard file to the measured factor.
