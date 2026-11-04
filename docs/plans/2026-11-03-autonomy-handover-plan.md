# Handover plan: from supervisor-run to daemon-run Automation

**Date:** 2026-11-03
**Version:** written at 9.71.0
**Status:** draft for the operator; each phase starts only on the operator's word
**Scope:** move the mechanical parts of running Automata (planning review, stage mechanics, lane fill, merge, ship, deploy, watch and tick) from the supervisor session into the daemon, in shadow mode first, with the supervisor staying as the monitor of exceptions, bugs and improvements. Written so another operator can follow the same pattern with their own settings.

## Where we are

Automated today: task execution, verification scripts, judge reviews, infrastructure retries, plan coverage, scope lint, the burn guard, capacity accounting, CI.

Done by hand by the supervisor in one working day (2026-11-02 to 11-03): eight releases (trial merge, conflict resolution, review fixes, changelog, README line, version, ship, parity issue, deploy), about twenty-five decompose reviews with rewrites in most, approvals and backend settings, four rebuilds of one stage, re-queues, one dead worker session, CI run cleanups, registry rows and lessons, lane filling.

## Principles

1. **Shadow first.** Every handed-over step runs in shadow (records what it would do, does nothing) and is compared with the supervisor's real action for a fixed number of cases before it is switched on.
2. **One switch per step.** Each step has its own mode (off, shadow, on) and a kill switch; turning it off returns the step to the supervisor with no data loss.
3. **Evidence to move on.** A phase ends with counts-only evidence against its exit criteria, not an opinion.
4. **Rules stay with people.** The operator keeps: the 10.0.0 tag, design decisions, configuration authority (LLM entries, defaults, repository settings), and anything that spends money outside the plan. The daemon never edits the project rules files.
5. **Everything is configuration.** Nothing in the handover is tied to this installation: modes, thresholds, lists and names are settings with documented defaults, so another operator can run the same sequence.
6. **The supervisor does not disappear.** It reviews exceptions, reads the shadow comparisons, files defects and improvements the same day, and tunes rules and templates from the findings.

## Phase 0: measure the baseline (before anything changes)

Record, counts only, per day and per release:

| Measure | Source |
|---|---|
| Decomposes approved without an edit / with edits / rewritten | decompose lint scorecard and the supervisor ledger |
| Merge fixes made by hand (docs, tests, lint, conflicts) | merge log and the registry row |
| Minutes a lane sat idle with eligible work | lane utilization view |
| Manual re-queues, session kills, stage rebuilds | supervisor ledger |
| Red CI runs needing action | CI log |
| Releases shipped and releases deployed by hand | changelog and deploy log |

The ledger is a counts-only file the supervisor updates at each tick. These numbers are the "before".

Dependencies: decompose lint (running), lane utilization view (queued). Until they exist the ledger is kept by hand.

## Phase 1: plans that need no review

**Moves:** the supervisor's decompose review.
**How:** the decompose lint rules and task templates run on every decompose in shadow, then enforce with the retry ladder (touch-up, one re-plan with feedback, then the stronger planner), routed by the planning routing rules.
**Exit:** ten consecutive decomposes approved with no supervisor edit, across at least three kinds of work (UI, config and surfaces, defect with tests); the lint scorecard shows the false-positive rate under the agreed threshold.
**Rollback:** lint mode back to shadow; planner back to the global setting.
**Needs:** decompose lint (running), planning routing (approved), priority by kind (approved).

## Phase 2: stages that run themselves

**Moves:** building, starting and rebuilding stages.
**How:** an Automation can start from a fork or a nested Automation; a blocked Automation that never started returns to draft; queued planning runs and queued tasks can be re-routed to another pool; a node can be drained for maintenance; the release arc activates and advances stage by stage.
**Exit:** two consecutive stages start and finish with no rebuild; one node upgrade done by drain with no failed task.
**Needs:** activation fix (running), planner and worker re-queue (approved), node drain (planned), automatic grouping tags, the Gantt and lane utilization views for observation.

## Phase 3: lanes that fill themselves

**Moves:** the supervisor's lane filling.
**How:** lane fill goes shadow to on in two steps. Local-model lanes first (mechanical work, no spend), then the cloud lane under the burn guard and the automatic lane count. Proposal-file and eligibility rules are settled first (drafts filed from chat have no proposal file today).
**Exit:** three days where idle minutes with eligible work stay under the threshold; the shadow log and the real fills agree on at least ninety percent of decisions.
**Rollback:** lane fill mode back to shadow.
**Needs:** the lane fill eligibility fix, verifier slots with the headroom check (so verification does not starve), the auto lane count.

## Phase 4: review and merge by the daemon

**Moves:** the trial merge, mechanical conflict resolution, review fixes, changelog and README entries, version, parity issue, registry row.
**How, in order:**
1. The judge separates blocking findings from documentation findings. Documentation and process findings become automatic corrective tasks instead of a block; a real code or security finding still blocks.
2. The merge pipeline runs in shadow: it builds the trial merge, resolves what is mechanical (version files, changelog and README lines, locale JSON), runs lint and tests, and writes the merge plan it would execute; the supervisor compares it with the manual merge.
3. For patch-level changes with a clean verdict the pipeline goes on; minor releases still need an explicit confirmation until the comparison is clean; the 10.0.0 tag stays with the operator.
4. The datawatch-app parity issue is filed by the merge step after the check that the PWA changed.
**Exit:** ten merges where the shadow plan equals the manual merge (or is better), including at least two with conflicts and two with PWA changes.
**Needs:** the checks (shipped), classification verdicts (approved), review-timeout state (approved), spec-read truncation fix (approved), the read-only report path (approved), protect-the-rules-files (blocked, to triage).

## Phase 5: ship and deploy

**Moves:** push, tag, CI watch, deploy, restart.
**How:** ship steps shadow to on (already built, default on once a release is finished; confirm the secret scan before push is in place first); deploy after ship drains the node, installs, restarts, health-checks, resumes in-flight planning, and rolls back on a failed check.
**Exit:** five releases pushed, tagged, watched and deployed with no manual step and no lost planning run.
**Needs:** secret scan before push (approved), post-ship hooks and deploy (approved), re-queue after restart.

## Phase 6: the watch and the tick in the daemon

**Moves:** the supervisor's twenty-minute watch and hourly tick.
**How:** the daemon supervisor loop runs the mechanical checks (idle lane, stuck session, red CI, merge ready, budget position, registry audit) as configured jobs with selectable model and style, and raises a plain question or alert only for exceptions. The supervisor reads the exception stream, not every tick.
**Exit:** one week where the loop's exception list matches what the supervisor would have raised, with no missed incident.
**Needs:** supervisor loop (approved), the stats pull for federated peers, the daemon-side CI watch.

## Phase 7: made usable by others

The onboarding guide and skills (final stage) turn this sequence into a procedure another operator can run on a new repository: inspect, propose settings, run in shadow, compare, switch on. The release audit and full validation placeholder checks the rules, the test matrix and the methodology before any 10.0.0 candidate.

## Metrics of success

| Measure | Baseline (Phase 0) | Target |
|---|---|---|
| Supervisor edits per decompose | most decomposes | under 0.1 |
| Manual steps per release | about 12 | 0 for patches, 1 confirmation for minors |
| Idle lane minutes with eligible work | hours per day | under 30 per day |
| Stage rebuilds | 4 in one day | 0 |
| Releases not deployed within an hour | several | 0 |
| Supervisor time on routine ticks | continuous | exceptions only |

## Open decisions for the operator

Each is asked separately, with a recommendation, when its phase is about to start:

1. Should minor releases need a confirmation from the operator at first? Recommendation: yes for the first ten, then automatic when the shadow comparison is clean.
2. Should the daemon push and tag, or only prepare them? Recommendation: push and tag, with the secret scan and the CI watch in place, because the supervisor already holds that authority.
3. Which planner handles security-sensitive work? Recommendation: start on the stronger planner and move to a local model when the scorecard supports it.
4. How long is the shadow period per phase? Recommendation: the exit criteria above, not a fixed time.

## Parity surface

| Surface | Touched by the handover | Note |
|---|---|---|
| REST | yes | each step's mode, status and comparison report |
| MCP | yes | the same reads and mode setting |
| CLI | yes | status, compare and mode commands per step |
| Comm channel | yes | summaries and exception alerts |
| YAML/config | yes | one mode and threshold block per step |
| PWA | yes | the Setup and policy, Resources and Inbox sub-tabs, the arc view and the Gantt |
| Android | yes | each operator-visible change gets a datawatch-app issue at its merge |
| iPhone | yes | same issues |

This plan document itself changes no code and no surface.

## Status

- Generated wrapping (change record and the five drafted artefacts: gate report, changelog draft, parity draft, tracker rows, registry row): built, unit-tested and documented in [howto/generated-artefacts.md](../howto/generated-artefacts.md); not yet validated on a live daemon. The datawatch-app parity issue is drafted in `docs/plans/parity-issues/generated-artefacts.md` and is filed by the supervisor at merge. Tracked in the [plans index](README.md).
