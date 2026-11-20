# Release plans and supervisor status

**Date:** 2026-11-19 (night update)
**Version:** none (status document; plan-document commits are exempt from the version bump)
**Replaces:** the 2026-11-15 plan listing and the 2026-11-19 morning open-work listing, both kept only in the supervisor scratch folder and now out of date.

Read this page for where the build stands: the two release plans, what shipped today, what is open and why, how the supervisor itself is run, and what is being done to make that cheaper. Titles lead; the short record id follows so a row can be found on the Automata page. Every open row has a **Kind**: Bug (a defect), Feature (new capability), Maintenance, or Other (design, audit, placeholder and release-stage records).

## Release plans

| Plan | State | Progress | Next up |
|---|---|---|---|
| Release 10.0 | in_progress | 289 of 406 done (71 percent) | {'automaton_id': 'a7f20001', 'title': 'Fix open high and critical CodeQL alerts'} |
| Release 11.0 | planned | 3 of 24 done (12 percent) | {'automaton_id': '14310001', 'title': 'Stale browser state cannot break the Sessions view-mode buttons'} |

Release 10.0 members by state: archived 314, completed 25, needs_review 22, draft 15, blocked 10, cancelled 7, failed 5, held 5, running 2, revisions_asked 1.

Open work in 10.0 by kind: Bug 40, Other 11, Feature 9.

Reading the progress figure honestly: on 2026-11-15 the plan read 286 of 318 done (89 percent). Today it reads 289 of 406 (71 percent). The total grew by 88 because new work (defects found while building, follow-ups, backfilled records) was attached to the plan, while the done count moved by only 3. The percentage fell because the plan grew, not because finished work was lost. Completed records are finished and waiting to be archived; some cannot be archived yet (see the supervisor loop section).

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

All open records now carry a title (a title is what the Automata page shows; about 60 drafts, blocked and failed records had none and showed no details until tonight). Two duplicate drafts and three stub records left from supervisor hotfixes were cancelled; their work is shipped.

### Running now (2)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Bug | 80 | Repair three failing PWA end-to-end stories | 2acf0001 |
| Bug | 80 | Lean sessions list datawatch tools and richer changelog draft | 60440001 |

### On hold (5)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Bug | 80 | Scan fix loop route contract, docs and parity coverage | fc920001 |
| Bug | 80 | Guard script rename and shutdown-cause stop-match check | ecc10001 |
| Bug | 80 | Fix docs_list_howtos returning no how-tos on a sandbox daemon | a4280001 |
| Bug | 80 | Fix web UI load failure in the sandbox daemon and fail fast in the runner | 21130001 |
| Bug | 80 | Mobile-fit nav icon clipping and server dropdown tap target | 9cdb0001 |

### Waiting for approval (decomposed) (22)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Bug | 90 | Keep planning Automata in planning across a daemon restart | 04b60001 |
| Bug | 70 | Release leaked capacity slots, sweep them, and show slots and lease state | af960001 |
| Bug | 60 | Supervisor watch worktree check uses the Automaton's own project | e0770001 |
| Bug | 20 | Plan-coverage unresolved-file extraction fix | 234e0001 |
| Bug | 20 | Working llm models add, remove and list on every surface | 10980001 |
| Bug | 20 | Launcher answers the development-channels confirmation and verifies the task text | cfb30001 |
| Bug | 20 | Schedule CLI goes through the daemon | 6acd0001 |
| Bug | 20 | The schedule list shows fires, last fire time and result for schedules that fire | 674d0001 |
| Bug | 20 | Whole-title changelog entries | f6430001 |
| Bug | 20 | Memory-bounded merge test runs | db6f0001 |
| Bug | 20 | Gate runner ignores launcher files and worktrees generate the docs-index embed asset | 440c0001 |
| Bug | 20 | Loaded-model retry on placement refusal | 52b40001 |
| Bug | 20 | Merged Automata reach Completed whatever their review state | 96180001 |
| Bug | 20 | Close the remaining CodeQL high and critical alerts | 0be70001 |
| Bug | 20 | Scan settings writes take effect, persist, and refuse what they cannot apply | 44230001 |
| Bug | 20 | Untracked verifier inputs are committed or named at task start | 0d0e0001 |
| Bug | 20 | Completion landing setting autonomous.completion_landing.enabled on every config surface | 2c5d0001 |
| Bug | 20 | Document priority_stalls in the Cursor MCP tools table and guard the table | 4ae00001 |
| Bug | 20 | Real evidence in the generated gate report | 04a00001 |
| Bug | 20 | Judge timeout hold replay, honest review state and large-diff judging | e3420001 |
| Bug | 20 | Decompose lint catches generated gate script faults | 88980001 |
| Bug | 20 | Confine the session-end reap to descendants of the session | 45f10001 |

### Spec revised, waiting for a fresh plan (1)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Feature | 85 | Supervisor session start and managed restart | 3b270001 |

### Drafts (not decomposed) (15)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Feature | 85 | One status view of a project's work in the order it will run, for a session | e1940001 |
| Other | 70 | Infrastructure faults in the judge and verifier are treated as findings against the work | 09c30001 |
| Other | 70 | The pi planner's replies fail to parse about one time in three | c8f50001 |
| Other | 65 | Tasks added with add-task have no pinned model and placement refuses them | 13cf0001 |
| Other | 60 | First CodeQL hardening round shipped and the scanner then reported twelve alerts | 08170001 |
| Other | 60 | CodeQL check stayed red for two hours after a fix, with no code cause | 32100001 |
| Feature | 60 | Make the judge and verifier time limit follow the size of the review and the backend | 94560001 |
| Other | 55 | Worker-written final-gate and verification scripts fail for the same three reasons | e81e0001 |
| Other | 50 | The pi pool cap of three does not match the two single-engine compute nodes | 526f0001 |
| Other | 40 | Settings that ship off or in shadow lack definitions entries | 8a3e0001 |
| Other | 40 | Unbounded reads in the MCP tools (question list and repair-dependencies return too much) | 64650001 |
| Feature | 20 | Make the Projects and Plans pages create through the shared floating button and a | 70ae0001 |
| Feature | 20 | Add a dependency to an existing task, and stop repair-dependencies returning the whole record | 929d0001 |
| Feature | 20 | Choose a supervisor session for a project and an optional plan at launch | f3540001 |
| Bug | 20 | Wire the roll-up performance guard script into a workflow (held until the fast Projects pages wo | 3c680001 |

### Blocked (10)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Bug | 90 | Fix the shadows: plan size and version, changelog source, stored comparison, missing-plan reason | f31d0001 |
| Bug | 90 | Residue cleanup after Automaton merge | 18990001 |
| Bug | 80 | Make the internal/autonomous race-detector run pass | d5060001 |
| Bug | 80 | Fix eBPF generated-code drift with pinned toolchain | e3060001 |
| Bug | 80 | Guard no-PWA specs and smoke-test the priority-stalls surfaces | 9c8f0001 |
| Feature | 20 | Move Automata configuration into Settings and replace the usage calculator with small dashboards | 9f8f0001 |
| Feature | 20 | Merge scripts in the repository with shadow pipeline comparison | 71f80001 |
| Bug | 20 | Remove a tracked symlink that holds an absolute home path | 2fa30001 |
| Feature | 20 | Configuration parity audit and CI gate | 70900001 |
| Other | - | BL433 — PWA performance audit under load (design/audit pass) | 049f4713 |

### Failed (5)

| Kind | Priority | Work | Record |
|---|---|---|---|
| Bug | 90 | Remove the remote reap PWA part and document its settings | c07b0001 |
| Bug | 90 | Document the remote reap settings in the config reference and implementation guide | 27720001 |
| Other | 80 | Plans migration: history rows, real titles, explained conflicts, full counts | bcf30001 |
| Bug | 80 | Scope the supervisor watch worktree check to datawatch worktrees | a92a0001 |
| Bug | 80 | Make the full release smoke pass on main | 87620001 |

## Release 11.0

Planned, 3 of 24 done. Members by state: archived 8, needs_review 6, blocked 5, draft 4, cancelled 1. The blocked ones are the model evaluation runs and designs, parked until after the 10.0 release by the operator.

| Kind | State | Work | Record |
|---|---|---|---|
| Other | blocked | BL422 — Claude model/effort tiering eval for dev-Automaton tasks (eval only, no policy change) | f6a38c29 |
| Other | blocked | BL437 — Recurring, hardware-aware LLM model/effort evaluation (design pass) | fea73c82 |
| Other | blocked | [eval] model-tiering 20261015-r1 | 38b69678 |
| Other | blocked | [eval] open-model-matrix 20261015-r10 | 3be9c0e5 |
| Other | blocked | [eval] open-model-matrix 20261015-r7 | 9a1a8666 |
| Other | draft | BL460 — Implement the unaddressed recommendations from the open-LLM research (PRD a2833a5e, comp | 404726a8 |
| Feature | draft | Design and gap report for running local-model agents | e3780001 |
| Other | draft | Harness enhancement planning documents (eval sweep, eval DAG nodes, red-team validator) | 0fb4e302 |
| Other | draft | Research and design: datawatch as infrastructure for Hermes Agent | 79890001 |
| Feature | needs_review | Actionable Automata Inbox | 18980001 |
| Feature | needs_review | Authentication and multi-user design proposal | 2fbf0001 |
| Feature | needs_review | Git hosts: settings, readiness, GitLab, release, CI watch and tags | 7bd50001 |
| Other | needs_review | Personal agents with personality and decision-making (held placeholder) | 2c9a27e2 |
| Other | needs_review | Register the individual-builder harness research and its enhancement proposals | 316c41e1 |
| Other | needs_review | Register the openrig.dev adoption research as a held item | 36729dd7 |

## Finished work that is not on main

Found tonight by comparing every archived, completed and cancelled Automaton with `main`. **42** of them have a branch whose changes are not in `main` (checked two ways: the commit patches are absent, and for ten spot-checked UI branches none of the distinctive added code lines exist in `main`). The Automata page shows these as finished or archived, so the work looks done while the product does not have it. This is why several things asked for in the web app are not visible: the compact header with server and scope icons (it replaces the two server dropdowns), consistent phone page layout across all views, the Automata Resources, Progress and Housekeeping restyle, plan status roll-up and plan rename and edit, the Sessions toolbar fixes, Automation parallel chains, and more. The branches still exist, locally and on the remote, so nothing is lost and all of it can be landed.

Of the 42, 3 merge cleanly into `main` today and 39 conflict (they are 5 to 34 days old; most need rebasing). The 2026-11-14 inventory (`2026-11-14-archived-unmerged-automata-inventory.md`) found the same problem and planned its fix; the daemon work from that plan (landing records, the unlanded-work list, keep and drop) shipped, but the old branches themselves were never landed.

| Date | Kind | Work | State | Against main | Record |
|---|---|---|---|---|---|
| 10-15 | Other | Ship v9.1.9: autonomous task session-name prefix closeout | archived | conflicts with main | dc6c597b |
| 10-18 | Other | Runs leave no debris: clean tests, harness self-cleanup, safe auto-commits | archived | conflicts with main | 0631deb1 |
| 10-18 | Other | Decompose: resolve every depends_on entry at store time and warn about plan paths naming m | archived | conflicts with main | a354e98a |
| 10-18 | Other | Flag undeclared file changes in Automata tasks: per-task visible warning + alert, optional | archived | conflicts with main | a8500001 |
| 10-18 | Other | (untitled record) | archived | conflicts with main | e2120001 |
| 10-19 | Other | Sonnet plan review before approval | archived | conflicts with main | 06120001 |
| 10-19 | Other | Fix the default PRD quality gate so it runs, validates its baseline and stops failing pass | archived | conflicts with main | 91dc0001 |
| 10-19 | Other | Voice transcription long-recording failure: root-cause fix + streaming long-form design pa | archived | conflicts with main | 928f0001 |
| 10-19 | Other | Automatic spec-to-plan coverage check after every decompose | archived | conflicts with main | bb590001 |
| 10-19 | Other | Close the documentation gaps for the shipped lifecycle-event notification sinks (v9.3.0) | archived | conflicts with main | f56e0001 |
| 10-20 | Other | Refuse LLM settings changes while a plan is running | archived | conflicts with main | 4ce40001 |
| 10-20 | Other | Fix long-recording voice transcription and design streaming long-form voice input | archived | conflicts with main | c2b60001 |
| 10-20 | Other | Fix PWA /api/schedules request flood | archived | conflicts with main | cece0001 |
| 10-30 | Feature | Deploy after ship: a configured post-ship build, install, restart and health check | archived | conflicts with main | 90d60001 |
| 11-02 | Bug | Deflake round five: await question raise notifications before temp dir cleanup | archived | conflicts with main | 4e190001 |
| 11-03 | Feature | Verifier cap above 1 requires a measured headroom check | archived | conflicts with main | b20f0001 |
| 11-07 | Other | Agent mail between peers: design pass | cancelled | merges cleanly | ac690001 |
| 11-08 | Bug | Send an empty system_tags array on summary rows and legacy list records, and align the doc | archived | conflicts with main | 13f00001 |
| 11-09 | Feature | Installable packs for standard tasks and Automaton templates | archived | conflicts with main | 72bc0001 |
| 11-11 | Bug | Pi stall reporting, fast failure and node preflight | archived | conflicts with main | 72d10109 |
| 11-11 | Bug | Finalise session records whose process is gone and make every session list agree | archived | conflicts with main | 72d1017e |
| 11-12 | Feature | Trace retention follows the session | archived | conflicts with main | a1a30001 |
| 11-13 | Bug | Route each task to a lane at admission | archived | conflicts with main | 2b340001 |
| 11-13 | Bug | Consistent phone page layout across all views | archived | conflicts with main | 33160001 |
| 11-13 | Bug | Core automation stability 3: planner avoids foreseeable task and check failures | archived | conflicts with main | 41050001 |
| 11-13 | Feature | Automata Resources, Progress and costs, Housekeeping in datawatch style | archived | conflicts with main | 6e980001 |
| 11-13 | Bug | Sessions toolbar fixes: closable Filters panel and icon view buttons | archived | conflicts with main | 6eac0001 |
| 11-13 | Feature | Plan status roll-up and plan rename/edit | archived | conflicts with main | 71660001 |
| 11-13 | Feature | Core automation stability 1: automatic gate repair | archived | conflicts with main | 8b7a0001 |
| 11-13 | Feature | Automation parallel chains, concurrency limit, decision-wait and stage gating | archived | conflicts with main | 9f830001 |
| 11-13 | Bug | Lane fill frees lanes held by idle Automata | archived | conflicts with main | d0460001 |
| 11-13 | Bug | Core automation stability 2: archive only merged work | archived | conflicts with main | e5700001 |
| 11-14 | Feature | Compact header: server and project/plan scope icons with modals | archived | conflicts with main | 1d470001 |
| 11-14 | Bug | Stop the tracker writing RTK instructions into rules files | archived | conflicts with main | 26720001 |
| 11-14 | Feature | Per-LLM and per-model usage hold and burn guard | archived | conflicts with main | 3a640001 |
| 11-15 | Bug | Archive-based trivy scan for the agent-base build and scan gate | archived | conflicts with main | 4f450001 |
| 11-17 | Bug | (untitled record) | cancelled | merges cleanly | 21030001 |
| 11-17 | Bug | (untitled record) | completed | conflicts with main | 964b0001 |
| 11-18 | Bug | Completed requires a landed branch | completed | conflicts with main | 769e0001 |
| 11-18 | Feature | Daemon-side priority stall wake with blocker and action | completed | conflicts with main | e19b0001 |
| 11-18 | Bug | Harden the paths behind the eight open scanner findings | completed | conflicts with main | eb010001 |
| 11-18 | Bug | Deterministic stop of the merge-shadow test load process group | completed | merges cleanly | f0e10001 |

The last five (completed, from 2026-11-17 and 2026-11-18) are recent merges still waiting on the supervisor, not old losses.

## Why the features you asked for are not being built

Reading the operator questions (127 answered, 48 withdrawn, 25 open) against the open work shows a second cause beside the unlanded branches. Priority comes from the Automaton's kind: `autonomous.priority_by_kind` is bug 80, maintenance 70, feature 20, other 0. The supervisor's standing mission (the operator's direction of 2026-11-18, quoted in a question answer) is to approve and run priority 80 and above first. A feature therefore sits at priority 20 and is never picked: the watch's stall floor is 80, so a priority 20 feature never even shows as stalled. Features asked for are filed, decomposed and left waiting for approval while bug fixes of the build tooling run. Examples at priority 20 and waiting: organize the Projects page by plan (7b700001), make the Projects and Plans pages create through the shared floating button (70ae0001), the actionable Automata inbox, the working-models commands on every surface, the schedule list showing fires and results, and the authentication design.

Operator decisions that the questions record and that the findings above touch:

- 2026-11-12, Projects tab: sub-tabs, "give me the list of tabs we would have and lets get the order right". Shipped: Overview, Plans, Records, Gantt, Unassigned, History, Events. The earlier documents named a Policy sub-tab and an Arcs sub-tab; neither is in the build (Arcs became Plans; Policy settings go to Settings by a later answer).
- 2026-11-12, "yes lets do a plan sub-tab in projects for plans": shipped.
- 2026-11-12, the phone server selector: the compact header with server and scope icons is one of the 42 branches not on main.
- 2026-11-17, "The backlogs are supposed to be tied to plans. So there should be a plan for everything": 441 Automata belong to no plan, and the Automaton that organizes the Projects page by plan is at priority 20.
- 2026-11-17, "Unless I say post 10.0 or 11.0 is all 10.0": every unshipped item belongs in 10.0 unless the operator parks it.
- 2026-11-11, the held final stages (parity backfill, dead-code audit, naming audit, integration guide, release close-out, definitions backfill, federated placement test, classification validation, release audit): kept held until every 10.0 bug and feature is done. They are now titled so they can be read.

Open questions to go through with the operator once the missing work is found (25):

| Group | Count | What is asked |
|---|---|---|
| Uncommitted files left in a worktree or task | 10 | Keep, commit or drop leftover files of finished or held Automata |
| A branch could not be brought up to date with main | 7 | Resolve by hand, retry later or leave alone. The branches are the old unlanded ones, for example the git hosts work (23 conflicting files) and the fast Projects and Automata pages (20 files) |
| A task changed protected paths | 3 | Approve or reject a change to a build file or rule file |
| Pi template tasks refused | 2 | Route template tasks to the model the nodes serve, allow a model swap, or keep overriding by hand. Recommended: route |
| Settings and daemon | 3 | The crash-loop lint rule, whether a new supervisor setting gets a PWA field, and a merge-shadow Automaton that is now mostly obsolete |

## Audit of everything asked for (plan)

Everything is now managed from datawatch, so the audit checks that each thing asked for exists there and is on `main`.

1. **Landing audit** (done): every Automaton against `main`, as above.
2. **Code presence audit** (mechanical, no model): for every archived or completed Automaton, test that the distinctive lines it added exist in `main`; list the ones that do not, including branches already removed.
3. **Registry and plan audit** (mechanical): every original tracker row against a plan and a live Automaton. The migration preview shows 920 rows already carried over, 18 not yet created and 30 name conflicts; 441 Automata belong to no plan.
4. **Request audit**: every operator request quoted in the handover plan, the question queue and the design documents against a record that is open or landed.
5. **Landing work**: rebase each conflicting branch as its own Automaton (operator-requested web app work first), merge the clean ones through the supervisor chain, and have the daemon refuse to archive an Automaton whose branch is not on `main` (the completed-requires-a-landed-branch Automaton, itself still unlanded).

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
