# Automation supervisor duties: proposal for the Automation coordinator

**Date:** 2026-10-25
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL609

Confirmed: yes — Q1 to Q8 are decided (see each Status line). Confirmation, 2026-10-25 about 19:55, operator, quoted: "Yes", in answer to "Do you want me to set Confirmed: yes and file its implementation PRD?". Scope of the confirmation: Step 1 only (triage record and poll-time pass). Later steps are filed one at a time. Mobile parity issues are not filed without the operator's word.

This proposal absorbs the eight supervisor duties the 2026-10-24 orchestration design listed under "Supervisor duties the Automation coordinator must absorb" (see `docs/plans/2026-10-14-automation-orchestration-design.md`) into the Automation coordinator. Each duty below keeps that list's number and title, records what Exists today versus what is Missing (verified against the code at this commit), and names the smallest change that closes the gap. Every number (times, counts, surfaces) that the design left to the operator is collected under Open questions for the operator.

## Scope

This work would touch, if implemented:

- `internal/autonomous` — the coordinator, the poll loop, CI watch, lane fill, capacity and wait reasons, retry policy, eligibility holds, and the decision log.
- `internal/session` — the rate-limit and completion pattern matching against tool output.
- `internal/config` — any new policy keys (alert times, retry behaviour, surfaces).
- `internal/mcp` and `internal/server` — only if Q4 lands surfaces on those channels.
- `scripts/` and `security/` — the accepted-risks registry tooling that duty 2 builds on.
- `docs/` and `docs/plans/README.md` — registry rows, plan records, reference docs.

Surfaces (config, REST, MCP, CLI, comm channel, PWA, mobile) that each duty needs are not fixed here; that is Q4. Implementation is out of scope for this document: it proposes no code, no configuration change, and no other file change.

## Duties

### 1. Watch CI for every workflow

**Exists today.** `runPRDCompletionCIWatch` in `internal/autonomous/ci_watch.go` runs only when a completed Automaton carries the ci-watch guardrail name, and it watches the last completed task's `PostVerifySHA` (recorded by `CapturePostVerifySHA` in `internal/autonomous/executor.go`), falling back to live HEAD, requiring only that the commit be reachable from a remote-tracking ref (the comment in `ci_watch.go` cites `git merge-base --is-ancestor` as the ancestry check). `defaultCIRuns` lists that sha's runs with `gh run list --commit` and a limit of 50, so every workflow on that commit is listed, not only the release one. `classifyCIRuns` separates failed from infra runs; infra runs are rerun with `gh run rerun --failed` (`defaultCIRerun`) under the infra retry budget, and failed runs go to a judge (`ciWatchJudge` with `ciWatchSuffix`) whose verdict may be policy, meaning an operator decides. A timed-out watch is recorded with `ciWatchPending` plus one follow-up via `ciWatchFollowUp`.

**Missing.** Nothing watches a push to main or a tag push; nothing lists the workflows on the head of main or on tags on each poll; no run is ever classified as hung; and `ci_watch.go` has no run deletion. A red workflow on a tag therefore surfaces only if that Automaton happened to trip the completion guardrail.

**Smallest change.** Build order Step 6: a poll-driven watch of the head of main and of each tag across all workflows, recording the same triage record Step 1 creates.

### 2. Security-failure exception

**Exists today.** `security/accepted-risks.yml` is the suppression registry. `scripts/check_accepted_risks.py` validates entries (required fields, `first_added` not after `added`, `first_added` immutable against a git ref through its `--base` flag, an expiry window). `scripts/gen_trivyignore.py` regenerates `.trivyignore` and splices the container entries into `docs/security-review.md` from the registry, with a regenerate-and-diff check. Staleness and alerting are handled by `scripts/compute_stale_risks.py`, `scripts/accepted_risks_daily_watch.py`, `scripts/apply_stale_risk_removal.py` and `scripts/dismissed_alerts_watch.py`.

**Missing.** No code recognises a security-workflow failure as a dependency or CVE failure, computes an advisory's age, or stops the coordinator for an operator decision on it. Nothing in the coordinator guards against a session writing `.trivyignore` directly; the only protection today is the regenerate-and-diff check in `scripts/gen_trivyignore.py`.

**Smallest change.** Build order Step 7: classify the security workflow's failure, test the advisory age and registry state, and on a match stop-and-report through the triage record instead of fixing or documenting.

### 3. Merge pending is visible and alerting

**Exists today.** `internal/autonomous/progress_backlog.go` reads the `, merge pending` suffix on a completed status (`mergePendingTail`) and counts such a status as not done, but no Go code in `internal` or `cmd` writes that suffix from git state. `PlanLaneFill` in `internal/autonomous/lane_fill.go` reports merge-needed Automata once (`LaneFillActionReportMerge`) and never starts them. `internal/autonomous/ci_watch.go` already tests commit ancestry with `git merge-base --is-ancestor`. The merge helpers are `scripts/merge-branches-bump-version.sh` and `scripts/check-merge-version.sh`. `internal/server/web/app.js` contains no merge-pending text.

**Missing.** No comparison of a completed Automaton's commit content with main (no such code was found); no merge-pending state in the Automation view; no alert after a configurable time; no safe-to-delete report for branches whose commits are all on main.

**Smallest change.** Build order Step 5: the commit-content comparison against main, the merge-pending state in the Automation view with the Q1-configured alert, and the safe-to-delete branch report.

### 4. Search before create

**Exists today.** `CreatePRD` in `internal/autonomous/manager.go` (about line 1796) runs the injection guard and backend and model validation only. `checkPendingDuplicate` in `internal/autonomous/automation_insert.go` refuses to insert the same Automaton twice into one Automation; that is a duplicate guard, not a search. Registry rows live in `docs/plans/README.md`. The `docs_search` MCP tool (`internal/mcp/docs.go`) searches documentation, not Automata.

**Missing.** Any search over Automaton titles and specs of every status, over registry rows, and over unmerged branches at create time; and any warning in the create response on any surface. A duplicate effort is discoverable only by an operator who happens to look.

**Smallest change.** Build order Step 2: a read-only overlap finder whose warnings return beside the create result on every surface creation reaches, without blocking.

### 5. Failed and blocked PRDs are triaged every poll

**Exists today.** `onMemberTerminal` (about line 249) in `internal/autonomous/automation_coordinator.go` acts when a member Automation's Automaton reaches a terminal status: it follows the member's `on_failed` or `on_blocked` edge through `advance`, and when no edge exists it records `blocked` (about line 673), which raises an alert through `alertFn`. Failed members seen before boot recovery finishes are deferred (`deferred`, replayed by `Ready`). `internal/autonomous/retry_policy.go` resolves budgets per category (infra, content, scope, policy) with Automaton, profile and global precedence, and consumes them when a task fails, before the Automaton reaches failed. `recordEndedSessions` in `internal/autonomous/reset_ends_sessions.go` records session end reasons on the Automaton.

**Missing.** No pass over failed or blocked members on the poll (the only entry point today is the lifecycle event); no alert for a standalone failed Automaton outside an Automation that I found; the blocked alert carries the reason text, but the worker must confirm whether it also carries the failed task's error and the recorded session end reason; no triage record of any kind.

**Smallest change.** Build order Step 1: the poll-time pass plus the single triage record that Steps 5, 6 and 7 build their alerts on.

### 6. A lane whose PRDs all wait on verifiers is idle

**Exists today.** `PlanLaneFill` in `internal/autonomous/lane_fill.go` takes the occupied lanes from its occupant Automata; `internal/autonomous/lane_fill_coordinator.go` builds the occupants from Automaton status (`laneFillRunning`) plus a grace window after a start. Shadow mode is the first shipped mode (`docs/plans/2026-10-24-lane-fill-shadow-mode.md`), so the coordinator records decisions and starts nothing yet. In `internal/autonomous/capacity.go`, `setWaiting` moves a task to `waiting_capacity` with a wait reason; `HandoffVerifyLease` (about line 431) hands over a verifier lease and `JudgeHolder` (about line 421) names judge holders. The holder kinds (task, verifier, judge, decompose, session) are in `internal/autonomous/capacity_titles.go`. Verifier policy lives in `internal/config/verifier_policy.go` (fixed, prefer-free, balance, gpu-only, claude-only) and `internal/autonomous/verifier_policy_record.go`; escalation in `internal/autonomous/verifier_escalation.go` (`DecideVerifierBackend`, `RecordVerifierOutcome`); queue waits in `internal/autonomous/verifier_stats.go`.

**Missing.** A Claude lane still counts as occupied while its Automaton has a running status, even when every active task on it waits for a verifier slot, so no start is planned there; and the lane fill evidence carries no wait reason, so the planner cannot tell "working" from "waiting".

**Smallest change.** Build order Step 3: a wait-on-verifier flag on each occupant so `PlanLaneFill` counts such a Claude lane as available under the Claude lane count and the usage policy.

### 7. Gate implementation on open design points

**Exists today.** `PRDHoldReason` in `internal/autonomous/lane_fill_eligibility.go` holds an Automaton when a spec line starts with `HOLD`, when the text says do not run or sequenced last, or when it is sequenced after a reference that does not resolve to a completed Automaton; held Automata are excluded from lane fill with a reason. `internal/autonomous/decision_assistant.go` escalates to the operator through its hooks. `RecordVerifierEscalation` in `internal/autonomous/verifier_escalation.go` writes to the Automaton decision log.

**Missing.** No structured operator decision anywhere: decisions have no id, no question, and no recorded answer, so no task can be gated on one. The only gate is the `HOLD` text pattern, and a task that depends on an undecided point starts unless its text happens to carry `HOLD`.

**Smallest change.** Build order Step 8: the structured decision entry (id, question, answer state) on the decision log wherever Q6 lands it, and a gate that refuses a task whose spec references an unanswered one.

### 8. Detectors match the tool's own error line

**Exists today.** `rateLimitPatterns` in `internal/session/manager.go` (about lines 136 to 165) are substring patterns such as `limit reached`, `quota exceeded`, `rate limit exceeded` and `usage limit will reset`, matched case-insensitively against each output line shorter than 2048 characters (about line 7360); they are not tied to any backend's own error line. Completion detection is `matchesCompletionPattern` in the same file (about line 66). The stall detectors are state and process based and do not match output text: `SessionIdleObservation.Stalled` in `internal/autonomous/session_idle.go` and `ExitSilenceTracker` in `internal/autonomous/exit_silence.go`.

**Missing.** No record of the exact error line each backend actually prints, so the rate-limit list is backend-agnostic and ordinary output that quotes a limit phrase can match a real detection.

**Smallest change.** Build order Step 4: capture real error lines into fixtures, then replace the generic list with per-backend anchored matches and a per-backend test that ordinary text containing the phrase does not match.

## Build order

Each step is one PRD-sized unit of work. A step that depends on an open question starts only after that answer is recorded in this document (status flipped from Open, under the question). Every step carries its own docs checklist and Parity surface per AGENT.md's planning rules, and a step that changes a REST or WebSocket contract needs a mobile parity issue filed for it.

**Step 1.** Triage record and poll-time pass (duty 5).
Depends on: Q1 (alert times) and Q2 (retry counts); no other step. Creates the single triage record — failed or blocked member, failed task, error text, session end reason, retry budget left — that Steps 5, 6 and 7 write to, and the poll-time pass that fills it.
Proposed status: Planned.
- Docs checklist: `CHANGELOG.md`; new triage/alert-time keys in `docs/config-reference.yaml`; a `docs/testing-tracker.md` section for the triage record surface if one lands on REST; `docs/operations.md` for the poll loop; README and doc index only if the record becomes operator-visible; full phase completion checklist.
- Parity surface (pending Q4, recommendation (b)): config keys, REST read of the record, MCP read, CLI read, comm-channel alert carrying the record, PWA record card.
- Mobile: the record is a new REST contract, so a mobile parity issue is needed wherever Q4 puts it.

**Step 2.** Search before create (duty 4).
Depends on: none. A read-only overlap finder checks the new title and spec against Automata of any status, registry rows, and unmerged branches, and returns warnings beside the create result on whatever surface creation reached; it never blocks.
Proposed status: Planned.
- Docs checklist: `CHANGELOG.md`; `docs/testing-tracker.md` section for the warning field; README/docs index only if the warning becomes a new operator-visible affordance; no new config keys expected; full phase completion checklist.
- Parity surface (pending Q4, recommendation (a)): REST warning in the create response, PWA shows it read-only; config, MCP, CLI and comm channel unchanged (creation already reaches them).
- Mobile: if the create response shape changes, that is a contract change and a mobile parity issue is needed.

**Step 3.** Verifier-wait lane occupancy (duty 6).
Depends on: Q4, and on lane fill running in shadow mode long enough to compare its recorded decisions with the supervisor's. Adds a wait-on-verifier flag to each occupant (the wait reason is already captured by `setWaiting`), so `PlanLaneFill` counts such a Claude lane as available under the Claude lane count and the usage policy.
Proposed status: Planned.
- Docs checklist: `CHANGELOG.md`; `docs/parity-status.md` if the lane state gains an operator-visible column; no new config keys (lane count and usage policy exist); full phase completion checklist.
- Parity surface (pending Q4, recommendation (b)): existing config keys, REST lane state, MCP read, CLI read, comm-channel alert when a lane is counted idle, PWA lane view.
- Mobile: exposure of the wait-reason field would be a contract change, so a mobile parity issue is needed if Q4 lands it.

**Step 4.** Backend error-line inventory, then anchored failure matching (duty 8).
Part one. Depends on: none. Capture the real error lines from each backend the daemon runs, in live runs, into fixtures under the test tree.
Part two. Depends on: part one. Replace the backend-agnostic substring list with per-backend anchored matches, and add one test per backend showing that ordinary text containing the phrase does not match. Live validation is required, not only unit tests.
Proposed status: Planned.
- Docs checklist: `docs/llm-backends.md` session-completion/error-line notes for each backend touched; `CHANGELOG.md`; `docs/testing-tracker.md` section for the fixtures; full phase completion checklist.
- Parity surface: none — matching is daemon-internal; no REST, MCP, CLI, comm channel, config, PWA or mobile surface changes.
- Mobile: N/A — no REST or WebSocket contract changes.

**Step 5.** Merge pending by commit content and a safe-to-delete report (duty 3).
Depends on: Step 1 (the alert shape), Q1 (the alert time) and Q7 (the commit comparison method). Implements the comparison against main, the merge-pending state in the Automation view with its alert, and the safe-to-delete report for branches whose commits are all on main.
Proposed status: Planned.
- Docs checklist: `CHANGELOG.md`; alert-time key in `docs/config-reference.yaml`; `docs/testing-tracker.md` section for the merge-pending state surface; `docs/howto/` entry for using the safe-to-delete report; `docs/flow/` note if the merge state is a new data flow; full phase completion checklist.
- Parity surface (pending Q4, recommendation (b)): config, REST state and report read, MCP read, CLI read, comm-channel alert and report, PWA merge-pending badge and report view.
- Mobile: new state and report fields in REST are a contract change, so a mobile parity issue is needed.

**Step 6.** Poll-driven CI watch for the head of main and each tag across all workflows (duty 1).
Depends on: Step 1 (the record it writes), Q3 (whether a hung step may be rerun without asking) and Q8 (whether superseded failed runs are deleted). Extends the existing completion-time watch so every poll lists the workflows on the head of main and on each tag, classifies failures and hung runs, and records them in the triage record.
Proposed status: implemented on this branch — the `autonomous.automation.repo_ci_watch` block, the poll, the classification, the actions, the per-episode alert and every read surface (REST, MCP, CLI, comm, PWA status line) are in, with the docs in `docs/config-reference.yaml`, the Repo CI watch section of `docs/implementation.md`, `docs/operations.md`, `docs/howto/automation-repo-ci-watch.md` and the `docs/plans/parity-issues/repo-ci-watch.md` row in `docs/parity-status.md`; the shipped version is recorded at merge — the minor bump is made at merge time (next minor above main), so no version number appears on this branch.
- Docs checklist: `CHANGELOG.md`; `docs/config-reference.yaml` for any watch/alert keys; `docs/operations.md` section describing the poll; `docs/testing-tracker.md` for the new surfaces; full phase completion checklist.
- Parity surface (pending Q4, recommendation (a)): config, REST read of the watch state, PWA read-only status line; alerts ride the existing alert surfaces.
- Mobile: only if the watch state lands as a new REST contract; if so, a mobile parity issue is needed.

**Step 7.** Security-failure exception (duty 2).
Depends on: Step 6 (for the classification of a security workflow run), Step 1 (the stop-and-report record), and Q5 (the clock for the 72-hour rule). On a qualifying dependency or CVE failure it stops, and reports the dependency, advisory id, publication date, allowlist entry and an upgrade-or-keep recommendation; suppressions stay in the registry, never in the generated ignore file.
Proposed status: implemented on this branch — the `autonomous.automation.security_failure.mode` key (shadow by default), the stop-and-report pass and every read surface (REST, MCP, CLI, comm, PWA card) are in, with the docs in `docs/howto/automation-security-failure.md`, `docs/operations.md`, `docs/security-review.md`, `docs/datawatch-definitions.md`, `docs/testing-tracker.md` and the `docs/plans/parity-issues/security-failure-exception.md` mobile issue; the shipped version is recorded at merge — the minor bump is made at merge time (next minor above main), so no version number appears on this branch.
- Docs checklist: `CHANGELOG.md`; `docs/security-review.md` note for the stop-and-report behaviour; `docs/operations.md` for the exception flow; `docs/testing-tracker.md` for the record surface; full phase completion checklist.
- Parity surface (Q4 answered: option (b) for every duty, per the operator's 2026-10-25 decision): the `autonomous.automation.security_failure.mode` key on every configuration channel (PUT /api/config, the generic MCP `config_set`, CLI `config set`, comm `configure`; shadow by default); REST read `GET /api/autonomous/security-failures`; MCP `security_failure_list`; CLI `datawatch autonomous security-failure`; comm `autonomous security-failure [run-id]`; the PWA Security failure card; and the mobile parity issue in `docs/plans/parity-issues/security-failure-exception.md`. The dates stay in the registry (`security/accepted-risks.yml`), so no separate date key is added.
- Mobile: the stop-and-report record would be a new contract if exposed on REST, so a mobile parity issue is needed.

**Step 8.** Structured operator decisions and the design-point gate (duty 7).
Depends on: Q6 (where decisions are recorded); otherwise independent, per Q4. Gives each decision an id, a question and a recorded answer, and gates a task whose spec references an undecided point until the answer is recorded.
Proposed status: Planned.
- Docs checklist: `CHANGELOG.md`; `docs/datawatch-definitions.md` glossary entry for a structured decision (new operator-facing concept); `docs/testing-tracker.md` for the decision surfaces; full phase completion checklist.
- Parity surface (pending Q4, recommendation (a)): config for the gate, REST read of the decision and of the gate state, PWA read-only decision line; MCP, CLI, comm channel and mobile unchanged.
- Mobile: N/A unless Q4 adds a contract; a decision written to the existing decision log would not add one.

### Follow-on steps added 2026-10-26: move the supervisor's own checks into the daemon

Source: the operator asked how much of the supervisor's 20-minute watch and hourly tick could be built into datawatch
automation. The supervisor's current checks are `scripts/supervisor-watch.py` and the hourly procedure in
`2026-10-17-supervisor-rules-and-lessons.md` section 11a. Operator, 2026-10-26, quoted: "Add them as follow-on steps
under BL609, remember we should be using DataWatch automation and debugging as we do so, where possible". These are
planned steps, not confirmed ones: the confirmation of 2026-10-25 covers Step 1 only, and each later step is filed one
at a time after its own confirmation.

**Dogfood rule (applies to Step 1 and to every step below).** Each step, when it ships, is switched on for the
supervisor's own loop the same day. The supervisor runs the new built-in in parallel with its script or its manual
check, compares the two outputs, files every difference or defect as a tracker row and a draft PRD, and retires the
manual check only after the outputs have agreed for a full day. The supervisor is the first user of every step, and
debugging it is part of shipping it.

**Step 9.** Daemon-side watch. Depends on: Step 1 (the triage record and poll pass), Steps 5 and 6 (CI watch and
merge pending), Q1 (the shared 30 minute alert default, decided). The poll raises, through the existing alert and
notify channels and with no model call, the findings `scripts/supervisor-watch.py` produces today: tmux server
restart, daemon not active, a model not resident on a registered node, a task newly failed or held for review, a
finished Automaton not merged, an idle lane with eligible work, a broken or silent project worktree, a verification
script that is not executable, a dirty main checkout, and red or stuck CI. The supervisor session then wakes on an
alert instead of on a timer. Switch-over: the script stays as the oracle and the two run side by side until they agree
for a day.
Proposed status: Planned (design not confirmed).
- Docs checklist: `CHANGELOG.md`; new watch keys in `docs/config-reference.yaml`; `docs/operations.md` for the watch;
  `docs/testing-tracker.md`; full phase completion checklist.
- Parity surface (Q4, the full set): config keys, REST read of the findings, MCP read, CLI read, comm-channel alert,
  PWA findings card. Mobile: a parity issue for the new REST contract.

**Step 10.** Lane-fill eligibility, then switch lane fill from shadow to on. Depends on: Step 9 (the same findings
feed the eligibility), the Q4 surfaces for lane fill, and shadow mode having run long enough to compare its decisions
with the supervisor's. Teaches lane fill what the supervisor knows and it does not (checked 2026-10-26 against the live
shadow decisions): skip stale Automata (no update in 72 hours, probes and old drafts), never run a spec that says HOLD
or is sequenced after something unshipped, run an implementation Automaton only when its proposal file carries a
`Confirmed: yes` line, check that main does not already carry what the spec asks for, require a valid registered
worktree, and take its lane counts from the LLM concurrency cap so there is one source of truth (it is configured for
3 Claude lanes while the live cap is 1). Also fix pool-evidence reading for a registered host whose certificate the
daemon does not trust. Switch-over: compare shadow decisions with the supervisor's for a day, then turn the mode to
`on` for the GPU lanes first.
Proposed status: Planned (design not confirmed).
- Docs checklist: `CHANGELOG.md`; the lane-fill section of `docs/operations.md`; `docs/config-reference.yaml`;
  `docs/testing-tracker.md`; full phase completion checklist.
- Parity surface: config, REST, MCP, CLI, comm channel and PWA already exist for lane fill; new eligibility reasons
  appear in the decisions list on each. Mobile: parity issue only if the decision record shape changes.

**Step 11.** Merge mechanics in the coordinator (the BL473 coordinator row). Depends on: Steps 9 and 10, and operator
design decisions that are NOT made yet. Moves the supervisor's merge procedure into the daemon: a trial merge in a
throwaway worktree, lint and the full tests with the tmux variables unset, a changelog entry taken from the lines the
branch added against its merge-base, a refusal to ship from a dirty main checkout, shipping only on a green exit code,
cancelling the Automaton record only after the ship succeeded, and the registry row update. A merge conflict is held
for a model or the operator; it is never guessed. Open design points for the operator before this step can be
confirmed: whether the daemon may push and tag by itself, which release number rule applies without a human, and
what a held conflict costs in model use.
Proposed status: Planned (design open).
- Docs checklist and parity surface: to be written when the design points above are answered.

## Open questions for the operator

None of the following is decided. Each starts a step only when recorded in this document.

### Q1. Default alert times (duties 1, 3 and 5)

**Options.** (a) One shared default for every alert, for example 30 minutes, the same default as the lane-fill idle alert (`DefaultLaneFillIdleAlertMinutes` in `internal/config/config.go`). (b) Per-duty defaults: triage failures alert on the poll that sees them, merge pending after a longer delay, CI pending at the watch timeout. (c) No default; alerts are off until configured.

**Recommendation.** (b): a failure blocks work and should alert at once, while merge pending needs a person and stops no lane; every value stays configurable under either reading.

**Status.** **Decided by the operator, 2026-10-25: option (a), one shared default of 30 minutes** ("1, 30 minutes"), for CI failures, merge pending and failed or blocked PRDs. It matches the lane-fill idle alert default; every value stays configurable.

### Q2. Retry count (duties 1 and 5)

**Options.** (a) Reuse the existing per-category budgets (infra, content, scope, policy) with no new count. (b) One automatic retry for infra-class failures and none for content, scope or security failures. (c) A higher fixed count.

**Recommendation.** (b): the duty asks for exactly one rerun of a hung step, and the existing infra budget already bounds retries.

**Status.** **Decided by the operator, 2026-10-25: option (a), reuse the existing per-category budgets** with no new count ("2 existing but leave open for future examination"). Kept open: whether the coordinator should have its own retry count, to be examined once it has run for a while.

### Q3. Whether a hung CI step may be rerun without asking (duty 1)

**Options.** (a) Yes: rerun once automatically and log it, for infra-classified or hung runs, never for security workflow runs or for a run whose judge verdict is policy. (b) Always ask first. (c) Yes for infra runs only, and ask for hung runs.

**Recommendation.** (a) with its exclusions: a hung run has no verdict yet, and one rerun changes no code.

**Status.** **Decided by the operator, 2026-10-25: option (a)** ("1"): a hung or infrastructure-classified CI run is rerun once automatically and the rerun is logged; never for security workflow runs, and never for a run whose judge verdict is policy.

### Q4. Surfaces each duty needs (config, REST, MCP, CLI, comm channel, PWA, mobile)

**Options.** (a) Minimum: config and REST, with the PWA showing read-only state. (b) The full parity set for each operator-visible duty: config, REST, MCP, CLI, comm channel, PWA, and a mobile parity issue wherever a REST or WebSocket contract changes. (c) Alerts only, on the existing alert surfaces, with no new commands.

**Recommendation.** (b) for duties 3, 5 and 6, which show operator-visible state and actions; (a) for duties 4, 7 and 8, which are config-driven checks with no new action; for duty 2, (b) limited to the stop-and-report record; for duty 1, (a) with the alerts riding the existing alert surfaces.

| Duty | config | REST | MCP | CLI | comm channel | PWA | mobile |
|---|---|---|---|---|---|---|---|
| 1. Watch CI | watch/alert-time keys | read of watch state | excluded (a) | excluded (a) | excluded (a); alerts on existing surfaces | read-only status line | no new contract expected; issue only if state lands on REST |
| 2. Security-failure exception | unchanged (registry holds dates) | read of the stop-and-report record | read of the record | excluded (b-limited) | report dispatch | record card | parity issue, since a record is a new contract |
| 3. Merge pending | alert-time keys | state and safe-to-delete report read | read tool | read command | alert plus report | merge-pending badge and report view | parity issue (new state fields) |
| 4. Search before create | no new keys expected | warning beside the create result | excluded (a) | excluded (a) | excluded (a) | read-only warning text | parity issue if the create response shape changes |
| 5. Triage every poll | triage and alert-time keys | triage record read | read tool | read command | alert carrying the record | triage record card | parity issue (new record contract) |
| 6. Verifier-wait lanes | existing lane count and usage policy keys | lane occupancy and wait reason | read tool | read command | idle/lane alert | lane view with wait reason | parity issue if the wait-reason field ships |
| 7. Design-point gate | gate toggle | decision and gate state read | excluded (a) | excluded (a) | excluded (a) | read-only decision line | no new contract if the decision log is reused |
| 8. Error-line matching | per-backend pattern source | no new surface | excluded (a) | excluded (a) | excluded (a) | unchanged | no change |

**Status.** **Decided by the operator, 2026-10-25: option (b) for every duty** ("2"): the full parity set (config, REST, MCP, CLI, comm channel, PWA, and a mobile parity issue wherever a REST or WebSocket contract changes) for each of the eight duties. The recommended mix above is superseded; the table shows the minimum each duty needs, and the operator's rule is the full set.

### Q5. Clock for the 72-hour security rule (duty 2)

**Options.** (a) The advisory's publication date from the scanner output. (b) The registry `first_added` date. (c) The date the security run first failed.

**Recommendation.** (a): the duty speaks of advisory age, and `first_added` records the project's own first suppression of it.

**Status.** **Closed by the operator, 2026-10-25:** the 72-hour security rule is withdrawn ("consume security patches when they are available"), so there is no clock to choose. See AGENT.md, Security-failure rule, and registry row 198.

### Q6. Where operator decisions are recorded (duty 7)

**Options.** (a) Entries on the existing Automaton decision log, each with an id and a recorded answer. (b) A new decision file under `docs/plans`. (c) A field on each Automaton.

**Recommendation.** (a): the verifier and retry paths already write to the Automaton decision log, and the gate only needs an id to reference.

**Status.** **Decided by the operator, 2026-10-25: option 1** ("1"): decisions are recorded in the decision channel's answer records, linked from the PRD decisions list; until the queue exists the design and decisions documents carry them.

### Q7. How merge pending compares commits (duty 3)

**Options.** (a) `git cherry` against main, comparing patch ids. (b) Comparing each commit's tree with main. (c) Comparing commit subject and count, which the duty rejects.

**Recommendation.** (a): it compares commit content and still matches after a rebase, where commit ids differ.

**Status.** **Decided by the operator, 2026-10-25: option (a), `git cherry` against main comparing patch ids** ("1, git cherry"). **Caveat found on 2026-10-24 for the implementation to handle:** a commit whose patch was changed while it was merged (conflict resolution, renumbering a version) shows as not on main even though its work landed, so the coordinator reports the cherry result and, as a second line, whether the branch's commit subjects are on main, and calls a branch merge pending only when the cherry result and the content check agree; a disagreement is shown to the operator as 'needs a look'.

### Q8. Whether superseded failed CI runs are deleted (duty 1)

**Options.** (a) Delete the superseded failed runs of the same workflow after a green run of the fix on a later commit, listing them in the triage record first. (b) Never delete, only report. (c) Delete only after an operator confirms each one.

**Recommendation.** (a), restricted to failed runs superseded by a green run of the same workflow, and only after the triage record has named them.

**Status.** **Decided by the operator, 2026-10-25: option (a)** ("1"): superseded failed runs of the same workflow are deleted after a green run of that workflow on a later commit, and only after the triage record has named them. The security-failure exception for suppressed CVEs no longer holds them back (see the 2026-10-25 security rule change).

## Parity surface

This document changes nothing on any surface; it names where future steps would land, each pending Q4.

- **REST** — no endpoint or contract is touched by this document; steps that add state or records would read them over REST, as Q4 decides.
- **MCP** — no tool is added or changed here; only Steps 3, 5 and 7's recommended (b) reads would extend `internal/mcp`.
- **CLI** — no command is added here; a read command would appear only where Q4 picks (b).
- **comm channel** — no new verb is proposed here; alert and report dispatch rides the existing surfaces.
- **YAML/config** — no key is introduced by this document; alert-time and gate keys would be added by Steps 1, 5 and 8 when Q4 lands them.
- **PWA** — no layout or behaviour is described as built; read-only state and record cards would appear only per Q4.
- **Android** — no client change in this document; wherever a step's REST or WebSocket contract changes, that step files its own mobile parity issue.
- **iPhone** — as Android: nothing here, and the owning step carries the parity issue.

## Status

| Item | Value |
|---|---|
| Proposal | Planned |
| Confirmed | yes, Step 1 only (operator, 2026-10-25); Q1 to Q8 decided |
| Date | 2026-10-25 |
