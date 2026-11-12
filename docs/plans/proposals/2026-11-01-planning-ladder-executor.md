# Planning ladder executor (internal/autonomous/planning_ladder.go)

Date: 2026-11-01
Status: Implemented (same tree)

## What it is

The executor for `config.PlanningLadder` (introduced with planning routing):
given the ordered planner chain a routing rule resolves to and the ladder's
budgets, produce a plan that passes the same deterministic checks decompose
already runs on its parse, spending:

1. one **initial** plan per planner,
2. up to `TouchUpMax` **touch-up** passes — targeted fixes of the flagged
   tasks only (the prompt names the flagged task titles and the findings,
   and must return the full plan again),
3. up to `ReplanMax` **replan** passes — full re-plans with the findings as
   feedback,
4. **escalation** to the next planner in the chain after
   `EscalateAfterAttempts` failed attempts at the current planner (or when
   the budgets ran out), restarting fresh and spending whatever budget is
   left,
5. and, when the chain and both budgets are exhausted: `Passed=false` with
   the last plan held, the planner that made it, and the full attempt
   record.

Public surface (`internal/autonomous/planning_ladder.go`):

- `PlannerFn` — the injected planner call:
  `func(ctx, backend, model, prompt string) (raw string, err error)`.
  All tests (`TestPlanningRouting*`, this package) run the real state
  machine against scripted raw plans; no LLM is called.
- `RunPlanningLadder(ctx, ladder config.PlanningLadder, planners
  []config.PlannerRef, spec string, plan PlannerFn, now func() time.Time) LadderResult` —
  the attempt machine; `now` is the ladder clock (defaults to `time.Now`),
  both guards (nil planner, empty chain) are handled without panicking.
- `LadderResult` — `Passed`, `Planner`, `FinalStories`, `Attempts`
  (`AttemptRecord` per attempt: 1-based `Index`, `Kind`, `At`, planner
  ref, `Outcome`, `Error`, `FindingCount`, `Findings`).
- Kinds: `LadderAttemptInitial`, `LadderAttemptTouchUp`,
  `LadderAttemptReplan`; outcomes: `LadderOutcomePass`,
  `LadderOutcomeFindings`, `LadderOutcomePlannerError`,
  `LadderOutcomeParseFailure`.

## Design decisions

**Injected planner, not a backend call.**
`PlannerFn` is the only seam. Alternatives considered: (a) call the
backend directly inside the ladder — rejected because every existing test
would need a live LLM, and backend resolution (which backend/model pair
each `PlannerRef` maps to, how the prompt is framed) is genuinely
per-planner policy the caller must own; (b) a larger interface (Init/
Stream/…) — rejected: the executor needs exactly one call, one raw string
back, and the raw output is parsed with the same `ParsePlanning` decompose
uses (`internal/autonomous/decompose.go`).

**One plan-shape per attempt.**
Every attempt returns ONE complete plan (not a diff). Alternatives
considered: a touch-up returning only the changed tasks — rejected:
re-serializing a partial plan back into the full story/task tree is where
lost fields live, and "replace only the matched tasks" is expressible and
checkable as a prompt instruction on top of a full answer instead.

**Findings = the same three deterministic checks decompose runs.**
`ladderFindings` (this file) flattens, in order: `LintPlanScope` problem
strings (`internal/autonomous/scope.go`), `CheckPlanCoverage` requirements
reported as missing or weakened (`internal/autonomous/plan_coverage.go`),
and `CheckPlanStructure` findings
(`internal/autonomous/plan_coverage_findings.go`). Any finding fails the
attempt. The ladder is the execution mechanism for a policy that already
has a check; it adds no new judgement, and therefore cannot disagree with
decompose about what a plan must look like.

**Budgets are shared across the chain.**
`TouchUpMax` and `ReplanMax` are counters on the whole ladder run, not
per-planner: a planner that escalates early leaves budget for the planner
it hands off to. A per-planner copy (the obvious alternative) would let a
bad first planner consume the budget that a better second planner needed —
precisely the failure the ladder exists to stop. `EscalateAfterAttempts`
(0 → 2) is per-planner: it answers "how many chances does THIS planner get"
before the chain moves on (the config comment in `internal/config/`
already documents it that way).

**Escalation beats spending the last budget at a dead end.**
If, with a next planner in the chain, either the budgets are exhausted or
the planner has burned its `EscalateAfterAttempts` allowance, the ladder
escalates rather than forcing one more weak touch-up/replan. The
alternative (always spend local budget before escalating) exists in spirit:
local budgets are spent first, so it never escalates while it still has a
chance to fix in place and a next planner to hand the leftovers to.

**Errors escalate too.**
A `planner_error` / `parse_failure` attempt has no plan to touch up; the
ladder's only sensible next move is the next planner (fresh initial), or,
at the end of the chain, a replay budget. Touching up with no plan would
mean inventing one — that is a replan by another name, so the code says
replan.

**Zero PRD context, by construction.**
`LintPlanScope` and `CheckPlanStructure` take a `*PRD`; the executor runs
at plan level without one and passes `&PRD{}`. Both checkers only read the
PRD for context that a plan-level execution does not have (writable dirs,
project tree root), and their text-level rules still run — which is what
"the same checks decompose runs" requires.

## Failure modes

- Planner errors and parse failures: recorded on the attempt (`Error`,
  outcome), never retried silently; the ladder moves or terminates.
- A planner that returns a plan with findings forever: bounded by the
  shared budgets + per-planner allowance, then terminated with
  `Passed=false`. No unbounded loop by construction of the state machine
  (both counters strictly decrease on every findings attempt).
- Context cancellation: checked between attempts, returns whatever the
  ladder holds with whatever attempts it recorded.
- `plan == nil` or `len(planners) == 0`: zero `LadderResult`
  (`Passed=false`, no attempts) — documented contract, covered by the
  guards.

## Testing

`internal/autonomous/planning_ladder_test.go` — `TestPlanningRouting*` in
this package:

- `…PassesOnFirstAttempt` — clean plan ends the ladder after one attempt,
  initial prompt is the bare spec, pass attributed to that planner;
- `…TouchUpFixesOnlyFlaggedTask` — one flagged task (t2, missing its
  B124 verification exemption), the touch-up prompt names t2's title + the
  finding and not the unflagged task, the fixed plan is accepted, t1 kept
  byte-identical;
- `…TouchUpExhaustedReplans` — touch-up budget spent, the replay budget
  carries the run (same planner, single chain);
- `…EscalatesToSecondPlanner` — first planner fails its allowance
  (initial + touch-up), run continues at planner B's fresh initial (bare
  spec, no findings carried over), pass attributed to B;
- `…ExhaustsAllPlanners` — both planners fail everything, budgets spent:
  `Passed=false`, four attempts across two planners, last plan held;
- `…PlannerErrorEscalates` — planner error recorded on the attempt and the
  ladder escalates to a parsing planner;
- `…ParseFailureReplans` — non-parsing raw output → `parse_failure`
  outcome + replay budget at the end of the chain.

All driven by scripted `PlannerFn` values; the real state machine, the
real prompt builder, and the real finding pipeline (`LintPlanScope`,
`CheckPlanCoverage`, `CheckPlanStructure`) run unmodified.

## Parity surface

| Surface | Status | Detail |
|---|---|---|
| REST | Included | `/api/autonomous/planning-routing` (GET, PUT), `/test` (POST), `/scorecard` (GET). |
| MCP | Included | Four tools proxy the REST routes, registered with caps. |
| CLI | Included | A `planning-routing` group; `set` merges flags onto the current config. |
| comm channel | Included | A routing verb with show and scorecard proxies to REST. |
| YAML/config | Included | Routing rules live in the autonomous config block and are editable as YAML. |
| PWA | Included | A Settings card with a test form, result and counts-only scorecard. |
| Android | Excluded | Not built in this release; a datawatch-app parity issue is filed at merge. |
| iPhone/iOS | Excluded | Not built in this release; tracked by the same parity issue. |
