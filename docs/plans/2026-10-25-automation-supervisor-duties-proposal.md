# Automation supervisor duties the coordinator must absorb (proposal)

**Date:** 2026-10-25
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL609

Confirmed: no. The operator has not confirmed this proposal, and no open point below is decided.

## Scope

This proposal names, duty by duty, the behaviour the Automation coordinator must absorb from
the human supervisor loop, what already exists, the smallest change that would close each gap,
and the build step that would make it. If built, the work would touch the Go packages
`internal/autonomous`, `internal/session`, `internal/config`, `internal/mcp` and
`internal/server`, the Python helpers under `scripts/`, the `security/accepted-risks.yml`
registry and its generated `.trivyignore`, and operator-visible state in the PWA. The
operator-interface surfaces (config, REST, MCP, CLI, comm channel, PWA) and a mobile parity
surface are candidates only; which of them each duty actually needs is left open in Q4.

Implementation is out of scope here. This document proposes and orders the work and records the
open points for the operator; it builds and ships nothing, and it changes no surface.

This is a planning document under `docs/plans/`. Its duty titles are preserved verbatim from
the coordinator requirements, and PRD otherwise appears below only as a code identifier, file
path or config key; every operator-facing concept is written as Automaton, Automata or
Automation per the project terminology rule.

## Duties

### 1. Watch CI for every workflow

**Exists today.** `runPRDCompletionCIWatch` in `internal/autonomous/ci_watch.go` runs only
when an Automaton completes, and only when the ci-watch guardrail name resolves for it. It
watches the last completed task's `PostVerifySHA` (via `prdLastPostVerifySHA`) and falls back
to live HEAD, and requires the commit to be reachable from a remote-tracking ref. `defaultCIRuns`
runs `gh run list --commit` for that sha with limit 50, so every workflow run on that commit is
listed, not only the release workflow. `classifyCIRuns` separates failed from infra runs; infra
runs are rerun with `gh run rerun --failed` (`defaultCIRerun`) under the infra retry budget;
failed logs go to a judge (`ciWatchJudge`, `ciWatchSuffix`) whose verdict may be policy, meaning
an operator decides. A timed-out watch is recorded as pending with one follow-up (`ciWatchPending`,
`ciWatchFollowUp`).

**Missing.** Nothing watches a push to main or a tag push; nothing lists the workflows on the head
of main or on tags on each poll; no run is classified as hung; `ci_watch.go` has no run deletion.

**Smallest change.** The build step that closes this gap is Step 6, poll-driven CI watch for the
head of main and each tag across all workflows, built on the triage and alert shape from Step 1
and the operator choices in Q3 and Q8.

### 2. Security-failure exception

**Exists today.** `security/accepted-risks.yml` is the registry. `scripts/check_accepted_risks.py`
validates entries (required fields, `first_added` not after `added`, `first_added` immutable against
a git ref through `--base`, and an expiry window). `scripts/gen_trivyignore.py` regenerates
`.trivyignore` and splices the container entries into `docs/security-review.md` from the registry,
with a regenerate-and-diff check. `scripts/compute_stale_risks.py`,
`scripts/accepted_risks_daily_watch.py`, `scripts/apply_stale_risk_removal.py` and
`scripts/dismissed_alerts_watch.py` handle staleness and alerts.

**Missing.** No code recognises a security-workflow failure as a dependency or CVE failure, computes
an advisory's age, or stops the coordinator for an operator decision on it; nothing in the
coordinator guards against a session writing `.trivyignore` directly, and the only protection is
the regenerate-and-diff check.

**Smallest change.** The build step that closes this gap is Step 7, the security-failure exception,
which reuses the security-workflow-run classification from Step 6 and the stop-and-report record
shape from Step 1, and applies the advisory date clock chosen in Q5.

### 3. Merge pending is visible and alerting

**Exists today.** `internal/autonomous/progress_backlog.go` reads a `, merge pending` suffix on a
completed status and counts that status as not done; no Go code in `internal` or `cmd` writes the
suffix from git state. `PlanLaneFill` in `internal/autonomous/lane_fill.go` reports merge-needed
Automata once each and never starts them. Completion records `PostVerifySHA`, and `ci_watch.go` tests
ancestry with `git merge-base --is-ancestor`. `scripts/merge-branches-bump-version.sh` and
`scripts/check-merge-version.sh` are the merge helpers. `internal/server/web/app.js` has no
merge-pending text.

**Missing.** A comparison of a completed Automaton's commit content with main (no such code found);
a merge-pending state in the Automation view; an alert after a configurable time; a safe-to-delete
report for branches whose commits are all on main.

**Smallest change.** The build step that closes this gap is Step 5, merge pending by commit content
and a safe-to-delete report, which reuses the alert shape from Step 1, the alert time from Q1 and
the comparison method from Q7.

### 4. Search before create

**Exists today.** `CreatePRD` in `internal/autonomous/manager.go` runs the injection guard and
backend and model validation only. `checkPendingDuplicate` in `internal/autonomous/automation_insert.go`
refuses to insert the same Automaton twice into one Automation; that is not a search. Registry rows
live in `docs/plans/README.md`. The `docs_search` MCP tool (`internal/mcp/docs.go`) searches
documentation, not Automata.

**Missing.** Any search over Automaton titles and specs of every status, registry rows and unmerged
branches, and any warning in the create response on any surface.

**Smallest change.** The build step that closes this gap is Step 2, search before create: a read-only
overlap finder that returns warnings beside the create result and never blocks.

### 5. Failed and blocked PRDs are triaged every poll

**Exists today.** `onMemberTerminal` in `internal/autonomous/automation_coordinator.go` acts when a
member Automaton reaches a terminal status. It follows the member's `on_failed` or `on_blocked` edge
through `advance`, and blocks the Automation with a reason when no edge exists (`blocked`, which
raises an alert through `alertFn`). Failed members seen before boot recovery finishes are deferred
(the `deferred` map, replayed once boot recovery is ready). `internal/autonomous/retry_policy.go`
resolves budgets per category (infra, content, scope, policy) with Automaton, profile and global
precedence, and consumes them when a task fails, before the Automaton reaches failed.
`recordEndedSessions` in `internal/autonomous/reset_ends_sessions.go` records session end reasons.

**Missing.** No pass over failed or blocked members on the poll (the entry point is the lifecycle
event); no alert for a standalone failed Automaton outside an Automation; the blocked alert carries
the reason text, and a worker must confirm whether it also carries the failed task's error and session
end reason; no triage record.

**Smallest change.** The build step that closes this gap is Step 1, the triage record and poll-time
pass, which creates the one triage record (failed or blocked member, failed task, error text, session
end reason, retry budget left) that Steps 5, 6 and 7 write to.

### 6. A lane whose PRDs all wait on verifiers is idle

**Exists today.** `PlanLaneFill` in `internal/autonomous/lane_fill.go` takes occupied lanes from
occupant Automata; `internal/autonomous/lane_fill_coordinator.go` builds occupants from Automaton
status (`laneFillRunning`) plus a grace window after a start. Shadow mode is the first shipped mode
(`docs/plans/2026-10-24-lane-fill-shadow-mode.md`). `setWaiting` in `internal/autonomous/capacity.go`
moves a task to `waiting_capacity` with a wait reason; `HandoffVerifyLease` hands over a verifier
lease; `JudgeHolder` names judge holders; the holder kinds task, verifier, judge, decompose and
session are in `internal/autonomous/capacity_titles.go`. Verifier policy is
`internal/config/verifier_policy.go` (fixed, prefer-free, balance, gpu-only, claude-only) and
`internal/autonomous/verifier_policy_record.go`. Escalation is `internal/autonomous/verifier_escalation.go`
(`DecideVerifierBackend`, `RecordVerifierOutcome`). Queue waits are in
`internal/autonomous/verifier_stats.go`.

**Missing.** A Claude lane counts as occupied while its Automaton has a running status, even when
every active task waits for a verifier slot, so no start is planned there; the lane evidence carries
no wait reason.

**Smallest change.** The build step that closes this gap is Step 3, verifier-wait lane occupancy: a
wait-on-verifier flag on the occupant, so `PlanLaneFill` counts such a Claude lane as available under
the Claude lane count and the usage policy.

### 7. Gate implementation on open design points

**Exists today.** `PRDHoldReason` in `internal/autonomous/lane_fill_eligibility.go` holds an
Automaton when a line starts with HOLD, when the text says do not run or sequenced last, or when it
is sequenced after a reference that does not resolve to a completed Automaton; held Automata are
excluded from lane fill with a reason. `internal/autonomous/decision_assistant.go` escalates to the
operator through its hooks. `RecordVerifierEscalation` in `internal/autonomous/verifier_escalation.go`
writes to the Automaton decision log.

**Missing.** No structured operator decision (no id, no question, no recorded answer), so no task can
be gated on one; the only gate is a text pattern, and a task that depends on an undecided point starts
unless its text carries HOLD.

**Smallest change.** The build step that closes this gap is Step 8, structured operator decisions and
the design-point gate, which starts only after Q6 is answered.

### 8. Detectors match the tool's own error line

**Exists today.** `rateLimitPatterns` in `internal/session/manager.go` are substring patterns such as
`limit reached`, `quota exceeded`, `rate limit exceeded` and `usage limit will reset`, matched
case-insensitively against each output line shorter than 2048 characters. They are not tied to a
backend's own error line. Completion detection is `matchesCompletionPattern` in
`internal/session/manager.go`. The stall detectors are state and process based and do not match text:
`SessionIdleObservation.Stalled` in `internal/autonomous/session_idle.go` and `ExitSilenceTracker`
in `internal/autonomous/exit_silence.go`.

**Missing.** No record of the exact error line each backend prints, so the rate-limit list is
backend-agnostic and ordinary output that quotes a limit phrase can match.

**Smallest change.** The build step that closes this gap is Step 4, backend error-line inventory then
anchored failure matching: part one captures real error lines from each backend into fixtures, and
part two replaces the backend-agnostic substring list with per-backend anchored matches and live
validation.

## Build order

Each step below is one Automaton-sized unit of work. A step that depends on an open question (Q1 to
Q8) starts only after that answer is recorded in this document, in the Open questions section below.
Each step carries its own docs checklist and Parity surface; a step that changes a REST or WebSocket
contract requires a `datawatch-app` mobile parity issue per the Mobile-Parity rule.

**Step 1.** Triage record and poll-time pass (duty 5).
It creates the one triage record (failed or blocked member, failed task, error text, session end
reason, retry budget left) that Steps 5, 6 and 7 write to; adds a pass over failed or blocked members
on every poll; alerts standalone failed Automata outside an Automation; and defines the alert shape the
later steps reuse.
- Depends on: Q1 for the alert time, and Q2 for the retry count.
- Docs checklist: CHANGELOG [Unreleased]; `docs/config-reference.yaml` for the new alert-time and
  retry keys; `docs/operations.md` for the triage and alerting behaviour; `README.md` and the doc index
  if an interface is added; `docs/testing-tracker.md` for the triage and alert contract; no internal
  tracker IDs in operator-facing copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (alert-time and retry keys), REST (triage record and
  standalone-failure alert), comm channel (alert), PWA (read-only triage list). MCP and CLI excluded
  here because the poll adds no new machine tool and no new operator command.
- Mobile parity: the triage and alert fields change a REST/WS contract, so a `datawatch-app` issue is
  required.
- Proposed status: Planned.

**Step 2.** Search before create (duty 4).
A read-only overlap finder checks the title and spec against Automata of any status, registry rows and
unmerged branches, and returns warnings beside the create result naming what it found; it never blocks.
- Depends on: none (no other step, no open question).
- Docs checklist: CHANGELOG [Unreleased]; `docs/config-reference.yaml` for the finder on/off key;
  `README.md` and the doc index if a surface is added; `docs/testing-tracker.md` for the create-response
  warning field; `docs/datawatch-definitions.md` if "overlap warning" becomes an operator-facing term; no
  internal tracker IDs in operator-facing copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (finder on/off), REST (warnings beside the create
  result), PWA (the warning shown at create). MCP excluded (the existing `docs_search` stays
  documentation-only); CLI and comm channel excluded because the finder is warn-on-create, not a command
  or alert.
- Mobile parity: the create response gains a warnings field, a REST contract change, so a `datawatch-app`
  issue is required.
- Proposed status: Planned.

**Step 3.** Verifier-wait lane occupancy (duty 6).
A wait-on-verifier flag on the occupant, so `PlanLaneFill` counts a Claude lane as available (so a
start is planned there) when every active task on that Automaton waits for a verifier slot, under the
Claude lane count and the usage policy; the lane evidence carries the wait reason.
- Depends on: lane fill running in shadow mode long enough to compare its decisions with the
  supervisor's, and Q4 for the surfaces.
- Docs checklist: CHANGELOG [Unreleased]; `docs/config-reference.yaml` if a lane-count or usage-policy
  key is added; `docs/operations.md` for the lane-fill behaviour change; `docs/flow/` if the lane
  occupancy data flow changes; `docs/testing-tracker.md` for the occupant evidence contract; no internal
  tracker IDs in operator-facing copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (lane count, usage policy), REST (occupant and wait-reason
  evidence fields), PWA (evidence panel), comm channel (the idle alert). MCP and CLI excluded because the
  flag is an internal scheduling input with no new machine tool or command.
- Mobile parity: the occupant evidence is a REST/WS contract change, so a `datawatch-app` issue is
  required.
- Proposed status: Planned.

**Step 4.** Backend error-line inventory, then anchored failure matching (duty 8).
Part one: with no dependency, capture the real error line each backend the daemon runs actually prints,
in live runs, into fixtures. Part two: replace the backend-agnostic substring list with per-backend
anchored matches, plus one test per backend showing that ordinary text containing the phrase does not
match; live validation is required, not only unit tests.
- Depends on: none for part one; part two depends on part one (no other step, no open question).
- Docs checklist: CHANGELOG [Unreleased]; `docs/llm-backends.md` with each backend's error line;
  `docs/config-reference.yaml` if the match set becomes config; `docs/operations.md` for the failure
  detection behaviour; `docs/testing-tracker.md` for the per-backend match contract; no internal tracker
  IDs in operator-facing copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (per-backend match fixtures), and only if the failed or
  reason text is surfaced, REST (session failed reason) and PWA (reason text). MCP, CLI and comm channel
  excluded because the change is internal detection with no new tool, command or alert.
- Mobile parity: required only if the session failed/reason REST or WS contract changes; otherwise N/A
  (config-only).
- Proposed status: Planned.

**Step 5.** Merge pending by commit content and a safe-to-delete report (duty 3).
Compare a completed Automaton's commit content with main (not by commit count), show it as merge pending
in the Automation view, alert after a configurable time, and report a branch whose commits are all on main
as safe to delete.
- Depends on: Step 1 for the alert shape, Q1 for the alert time, and Q7 for the comparison method.
- Docs checklist: CHANGELOG [Unreleased]; `docs/config-reference.yaml` for the alert-time and compare
  method; `docs/howto/` for the merge-pending workflow; `docs/operations.md`; `docs/testing-tracker.md`
  for the merge-pending state and safe-to-delete contract; no internal tracker IDs in operator-facing copy;
  the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (alert time, compare method), REST (merge-pending state
  and safe-to-delete report in the Automation view), PWA (merge-pending chip and alert), comm channel
  (the alert), CLI (the safe-to-delete report). MCP excluded because it is a read-only view plus report,
  not a new machine tool.
- Mobile parity: the Automation view and report are REST/WS contract changes, so a `datawatch-app` issue
  is required.
- Proposed status: Planned.

**Step 6.** Poll-driven CI watch for the head of main and each tag across all workflows (duty 1).
On each poll, list every workflow run on the head of main and on each tag after a push or tag; read and
classify a failed or hung run, fix through a fix Automaton or report the failing job and log excerpt;
rerun a hung step once under the infra budget; never show a red workflow as running; and handle
superseded failed runs per Q8.
- Depends on: Step 1 for the triage and alert shape, Q3 for hung-step reruns, and Q8 for superseded
  failed runs.
- Docs checklist: CHANGELOG [Unreleased]; `docs/config-reference.yaml` for the watch timeout, rerun and
  delete policies; `docs/operations.md` for the CI watch behaviour and log handling;
  `docs/testing-tracker.md` for the per-workflow state contract; no internal tracker IDs in operator-facing
  copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (watch timeout, rerun and delete policies), REST
  (per-workflow run state on the head of main and on tags), PWA (the workflow matrix), comm channel
  (the alert), CLI (list and rerun). MCP excluded unless Q4 assigns a machine tool.
- Mobile parity: the per-workflow run state is a REST/WS contract change, so a `datawatch-app` issue is
  required.
- Proposed status: Planned.

**Step 7.** Security-failure exception (duty 2).
Recognise a dependency or CVE failure from a security workflow for an advisory already suppressed as
older than 72 hours; never fix or document it; stop and send the operator the dependency, advisory id,
publication date, allowlist entry and an upgrade-or-keep recommendation; and keep suppressions in the
accepted-risks registry only (never the generated ignore file).
- Depends on: Step 6 for classifying security workflow runs, Step 1 for the stop-and-report record, and
  Q5 for the advisory date clock.
- Docs checklist: CHANGELOG [Unreleased]; `SECURITY.md` and `docs/operations.md` for the stop-and-report
  behaviour; `docs/config-reference.yaml` for the 72-hour clock key; `docs/testing-tracker.md` for the
  stop-and-report record contract; no internal tracker IDs in operator-facing copy; the Parity surface
  below stays accurate.
- Parity surface (proposed, pending Q4): config (the 72-hour clock, registry-only policy), comm channel
  (the stop-and-report record), REST (the record) and PWA (the record view). MCP and CLI excluded because
  the duty is a stop-and-report with no new machine tool or command.
- Mobile parity: if the stop-and-report record is exposed on a REST/WS contract, a `datawatch-app` issue
  is required.
- Proposed status: Planned.

**Step 8.** Structured operator decisions and the design-point gate (duty 7).
Record operator decisions as structured entries (an id, the question, and the recorded answer); gate a
task that depends on an undecided point on that decision, and otherwise stop and report the missing
decision, replacing the text-pattern-only gate.
- Depends on: Q6 for where operator decisions are recorded; otherwise independent.
- Docs checklist: CHANGELOG [Unreleased]; `docs/datawatch-definitions.md` if "operator decision" becomes
  an operator-facing term; `docs/config-reference.yaml` for the gate on/off key; `docs/howto/` for recording
  a decision; `docs/testing-tracker.md` for the decision record contract; no internal tracker IDs in
  operator-facing copy; the Parity surface below stays accurate.
- Parity surface (proposed, pending Q4): config (decision location per Q6, gate on/off), and if the record
  surfaces, MCP (a record/answer tool), REST (the decision record) and PWA (the decision card). CLI and
  comm channel excluded unless Q4 assigns them.
- Mobile parity: required only if the decision record is exposed on a REST/WS contract; otherwise N/A
  (config/MCP only).
- Proposed status: Planned.

## Open questions for the operator

### Q1. Default alert times (duties 1, 3 and 5)
**Options.**
- (a) one shared default for every alert, for example 30 minutes, the same default as the lane-fill idle
  alerts (`DefaultLaneFillIdleAlertMinutes` in `internal/config/config.go`);
- (b) per-duty defaults, with triage failures alerting on the poll that sees them, merge pending after a
  longer delay, and CI pending at the watch timeout;
- (c) no default, alerts off until configured.
**Recommendation.** (b): a failure blocks work and should alert at once, while merge pending needs a
person and stops no lane, and every value stays configurable.
**Status.** Open.

### Q2. Retry count (duties 1 and 5)
**Options.**
- (a) reuse the existing per-category budgets (infra, content, scope, policy) with no new count;
- (b) one automatic retry for infra-class failures and none for content, scope or security failures;
- (c) a higher fixed count.
**Recommendation.** (b): the duty asks for one rerun of a hung step, and the existing infra budget already
bounds retries.
**Status.** Open.

### Q3. Whether a hung CI step may be rerun without asking (duty 1)
**Options.**
- (a) yes, rerun once automatically and log it, for infra-classified or hung runs, never for security
  workflow runs or for a run whose judge verdict is policy;
- (b) always ask first;
- (c) yes for infra runs only, and ask for hung runs.
**Recommendation.** (a) with its exclusions: a hung run has no verdict yet, and one rerun changes no code.
**Status.** Open.

### Q4. Surfaces each duty needs (config, REST, MCP, CLI, comm channel, PWA, mobile)
**Options.**
- (a) minimum: config and REST, with the PWA showing read-only state;
- (b) the full parity set for each operator-visible duty: config, REST, MCP, CLI, comm channel, PWA, and a
  mobile parity issue wherever a REST or WebSocket contract changes;
- (c) alerts only, on the existing alert surfaces, with no new commands.

Proposed surface per duty (each cell names the proposed surface; MCP and CLI are left empty where the
duty adds no machine tool and no command):

| duty | config | REST | MCP | CLI | comm channel | PWA | mobile |
|---|---|---|---|---|---|---|---|
| 1 | watch timeout, rerun and delete policy | per-workflow run state | — none — | list and rerun | alert | workflow matrix | parity issue |
| 2 | 72-hour clock, registry-only | stop-and-report record | — none — | — none — | stop-and-report | record view | parity issue |
| 3 | alert time, compare method | merge-pending state, safe-to-delete report | — none — | safe-to-delete | alert | merge-pending chip | parity issue |
| 4 | finder on/off | create-response warnings | — none — | — none — | — none — | warning at create | parity issue |
| 5 | alert time, retry count | triage record, standalone-failure alert | — none — | — none — | alert | triage list | parity issue |
| 6 | lane count, usage policy | occupant and wait-reason evidence | — none — | — none — | idle alert | evidence panel | parity issue |
| 7 | decision location, gate on/off | decision record | record and answer | — none — | — none — | decision card | parity issue if REST |
| 8 | per-backend match fixtures | failed reason (if surfaced) | — none — | — none — | — none — | reason text | parity issue if REST |

**Recommendation.** (b) for duties 3, 5 and 6, which show operator-visible state and actions; (a) for
duties 4, 7 and 8, which are config-driven checks with no new action; for duty 2, (b) limited to the
stop-and-report record.
**Status.** Open.

### Q5. Clock for the 72-hour security rule (duty 2)
**Options.**
- (a) the advisory's publication date from the scanner output;
- (b) the registry `first_added` date;
- (c) the date the security run first failed.
**Recommendation.** (a): the duty speaks of advisory age, and `first_added` records the project's own
first suppression.
**Status.** Open.

### Q6. Where operator decisions are recorded (duty 7)
**Options.**
- (a) entries on the existing Automaton decision log (the one the retry and verifier paths already write),
  each with an id and a recorded answer;
- (b) a new decision file under `docs/plans`;
- (c) a field on each Automaton.
**Recommendation.** (a): the verifier and retry paths already write to the Automaton decision log, so a task
can be gated on the same record.
**Status.** Open.

### Q7. How merge pending compares commits (duty 3)
**Options.**
- (a) `git cherry` against main, comparing patch ids;
- (b) comparing each commit's tree with main;
- (c) comparing commit subject and count, which the duty rejects.
**Recommendation.** (a): it compares commit content and still matches after a rebase, where commit ids
differ.
**Status.** Open.

### Q8. Whether superseded failed CI runs are deleted (duty 1)
**Options.**
- (a) delete the superseded failed runs of the same workflow after a green run of the fix on a later
  commit, listing them in the triage record first;
- (b) never delete, only report;
- (c) delete only after an operator confirms each one.
**Recommendation.** (a), restricted to failed runs superseded by a green run of the same workflow.
**Status.** Open.

## Parity surface

This planning document changes nothing on any surface; each surface is listed with the reason. The
surfaces each duty actually needs are left to Q4.

- **REST:** unchanged — this document only proposes behaviour and build order; no endpoint or field is
  added until a step ships.
- **MCP:** unchanged — no tool is added or renamed here; any new tool remains proposed, not built.
- **CLI:** unchanged — no command is added here.
- **comm channel:** unchanged — no message is emitted here; the stop-and-report and alert copy are
  proposed, not sent.
- **YAML/config:** unchanged — the new keys are named in the steps but not written to config or
  `docs/config-reference.yaml`.
- **PWA:** unchanged — `internal/server/web/app.js` is cited only to record the current merge-pending
  gap; no UI is changed here.
- **Android:** unchanged — a planning document touches no mobile surface.
- **iPhone:** unchanged — a planning document touches no mobile surface.

The operator-interface surfaces each duty needs (config, REST, MCP, CLI, comm channel, PWA) and whether
a mobile parity issue is owed are decided in Q4.

## Status

| Item | Value |
|---|---|
| Proposal | Automation supervisor duties the coordinator must absorb |
| Status | Planned |
| Confirmed | no |
| Date | 2026-10-25 |
