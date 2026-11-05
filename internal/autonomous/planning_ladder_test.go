package autonomous

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Every TestPlanningRouting* test below drives the REAL
// RunPlanningLadder state machine (initial → touch-up → replan →
// escalate → exhausted) with a scripted PlannerFn: no LLM is called, and
// every prompt the executor sends is captured so the assertions prove
// WHICH step the ladder stepped to and with what feedback (task 5:
// "tests ... avoid real LLM calls" + "touch-up passes (sending the
// flagged task titles and findings)"). What each test guards:
//
//   - PassesOnFirstAttempt      — a clean plan ends the ladder after
//     exactly one attempt, attributed to the planner that produced it;
//   - TouchUpFixesOnlyFlagged   — the ladder spends a touch-up pass on
//     the SAME planner, names ONLY the flagged task (plus the findings)
//     in the prompt, and accepts the fixed plan; it must NOT replan or
//     escalate while touch-up budget remains;
//   - TouchUpExhaustedReplans   — after the touch-up budget (default 1)
//     is spent the ladder replans with the feedback, not escalate (a
//     single-planner chain has nothing to escalate to);
//   - EscalatesToSecondPlanner  — after EscalateAfterAttempts (default
//     2) failed attempts, the ladder starts fresh at the next planner
//     and the pass is attributed to that planner;
//   - ExhaustsAllPlanners       — chain and both budgets spent: the
//     ladder terminates with Passed=false, the full attempt record and
//     the last plan it holds;
//   - PlannerErrorEscalates     — a planner error is a failed attempt
//     with no plan to touch up: the ladder escalates (never a touch-up
//     with nothing to fix) and records the error on the attempt;
//   - ParseFailureReplans       — a non-parsing raw output fails the
//     attempt as parse_failure, and at the end of the chain the ladder
//     spends a replay budget on a fresh prompt.

// Ladder test fixtures. The plan is deliberately token-free (no file
// references, no tracker ids, no write-verb task titles) so the ONLY
// finding the flagged variant can draw is the one the test is about:
// t2 carrying neither verification_script nor verification_script_reason
// (structural rule 4), which names t2 and no other task.
const ladderTestSpec = "Give a consistent budget to the plan retry loop."

const ladderCleanPlanJSON = `{
  "title": "Ladder budget plan",
  "stories": [
    {
      "id": "st-ladder",
      "title": "Ladder attempt surface",
      "tasks": [
        {"id": "t1", "title": "Track remaining touch-up and replay budgets", "spec": "Count the touch-up and replay passes the ladder still may spend.", "verification_script_reason": "covered by the ladder test suite"},
        {"id": "t2", "title": "Record the planner chain on every attempt", "spec": "Attribute each ladder attempt to the planner that produced it.", "verification_script_reason": "covered by the ladder test suite"}
      ]
    }
  ]
}`

// ladderFlaggedPlanJSON — the same plan with t2's verification
// exemption removed: exactly one structural finding, naming t2.
const ladderFlaggedPlanJSON = `{
  "title": "Ladder budget plan",
  "stories": [
    {
      "id": "st-ladder",
      "title": "Ladder attempt surface",
      "tasks": [
        {"id": "t1", "title": "Track remaining touch-up and replay budgets", "spec": "Count the touch-up and replay passes the ladder still may spend.", "verification_script_reason": "covered by the ladder test suite"},
        {"id": "t2", "title": "Record the planner chain on every attempt", "spec": "Attribute each ladder attempt to the planner that produced it."}
      ]
    }
  ]
}`

// ladderFixedPlanJSON — the touch-up's answer: t2 gained its exemption,
// ONLY t2's spec text changed; t1 is byte-identical to the flagged plan
// ("replace only the matched tasks").
const ladderFixedPlanJSON = `{
  "title": "Ladder budget plan",
  "stories": [
    {
      "id": "st-ladder",
      "title": "Ladder attempt surface",
      "tasks": [
        {"id": "t1", "title": "Track remaining touch-up and replay budgets", "spec": "Count the touch-up and replay passes the ladder still may spend.", "verification_script_reason": "covered by the ladder test suite"},
        {"id": "t2", "title": "Record the planner chain on every attempt", "spec": "Attribute each ladder attempt to the planner that produced it, and keep the fixed attempt record.", "verification_script_reason": "covered by the ladder test suite"}
      ]
    }
  ]
}`

const (
	ladderPlannerA      = "claude-code"
	ladderPlannerAModel = "claude-opus-5-5"
	ladderPlannerB      = "pi-prompt"
	ladderPlannerBModel = "qwen3.8:27b"
)

var (
	ladderRefA = config.PlannerRef{Backend: ladderPlannerA, Model: ladderPlannerAModel}
	ladderRefB = config.PlannerRef{Backend: ladderPlannerB, Model: ladderPlannerBModel}
)

var ladderFixedClock = time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)

func ladderFixedNow() time.Time { return ladderFixedClock }

type ladderCall struct {
	backend, model, prompt string
}

// runLadder captures every plan call and returns the result, for
// compact assertions.
func runLadder(t *testing.T, ladder config.PlanningLadder, planners []config.PlannerRef, plan PlannerFn) (LadderResult, []ladderCall) {
	t.Helper()
	var calls []ladderCall
	rec := func(ctx context.Context, backend, model, prompt string) (string, error) {
		calls = append(calls, ladderCall{backend: backend, model: model, prompt: prompt})
		return plan(ctx, backend, model, prompt)
	}
	res := RunPlanningLadder(context.Background(), ladder, planners, ladderTestSpec, rec, ladderFixedNow)
	return res, calls
}

// expectKinds asserts the exact attempt-kind sequence of the ladder run.
func expectKinds(t *testing.T, res LadderResult, kinds ...string) {
	t.Helper()
	if len(res.Attempts) != len(kinds) {
		t.Fatalf("expected %d attempts (%v), got %d (%v)", len(kinds), kinds, len(res.Attempts), attemptKinds(res))
	}
	for i, k := range kinds {
		if res.Attempts[i].Kind != k {
			t.Fatalf("attempt %d kind = %q, want %q (sequence %v)", i+1, res.Attempts[i].Kind, k, attemptKinds(res))
		}
	}
}

func attemptKinds(res LadderResult) []string {
	out := make([]string, len(res.Attempts))
	for i, a := range res.Attempts {
		out[i] = a.Kind
	}
	return out
}

func taskByID(stories []Story, id string) (Task, bool) {
	for _, s := range stories {
		for _, t := range s.Tasks {
			if t.ID == id {
				return t, true
			}
		}
	}
	return Task{}, false
}

func TestPlanningRoutingPassesOnFirstAttempt(t *testing.T) {
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA},
		func(ctx context.Context, backend, model, prompt string) (string, error) {
			return ladderCleanPlanJSON, nil
		})

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	if !reflect.DeepEqual(res.Planner, ladderRefA) {
		t.Fatalf("Planner = %+v, want %+v", res.Planner, ladderRefA)
	}
	expectKinds(t, res, LadderAttemptInitial)
	if res.Attempts[0].Outcome != LadderOutcomePass {
		t.Fatalf("outcome = %q, want pass", res.Attempts[0].Outcome)
	}
	if res.Attempts[0].FindingCount != 0 || len(res.Attempts[0].Findings) != 0 {
		t.Fatalf("passing attempt carries findings: %v", res.Attempts[0].Findings)
	}
	if !res.Attempts[0].At.Equal(ladderFixedClock) {
		t.Fatalf("attempt At = %v, want the injected clock %v", res.Attempts[0].At, ladderFixedClock)
	}
	if len(res.FinalStories) != 1 || len(res.FinalStories[0].Tasks) != 2 {
		t.Fatalf("FinalStories = %+v", res.FinalStories)
	}
	// The initial prompt IS the bare spec: a clean plan must need nothing more.
	if len(calls) != 1 || calls[0].prompt != ladderTestSpec {
		t.Fatalf("calls = %+v, want the single bare-spec call", calls)
	}
	if calls[0].backend != ladderPlannerA || calls[0].model != ladderPlannerAModel {
		t.Fatalf("called planner = %s/%s, want %s/%s", calls[0].backend, calls[0].model, ladderPlannerA, ladderPlannerAModel)
	}
}

func TestPlanningRoutingTouchUpFixesOnlyFlaggedTask(t *testing.T) {
	// planner A: first answer flagged (t2 unverified), touch-up answer fixed.
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA},
		func(ctx context.Context, backend, model, prompt string) (string, error) {
			if strings.Contains(prompt, "TOUCH-UP") {
				return ladderFixedPlanJSON, nil
			}
			return ladderFlaggedPlanJSON, nil
		})

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	expectKinds(t, res, LadderAttemptInitial, LadderAttemptTouchUp)
	if res.Attempts[0].Outcome != LadderOutcomeFindings {
		t.Fatalf("first outcome = %q, want findings", res.Attempts[0].Outcome)
	}
	if res.Attempts[0].FindingCount != 1 {
		t.Fatalf("first FindingCount = %d (findings %v), want exactly 1", res.Attempts[0].FindingCount, res.Attempts[0].Findings)
	}
	if !strings.Contains(res.Attempts[0].Findings[0], "t2") {
		t.Fatalf("finding must name the flagged task t2: %q", res.Attempts[0].Findings[0])
	}
	if len(res.Attempts[0].Findings) != 1 || res.Attempts[0].Findings[0] != expectedLadderMissingVerificationFinding {
		t.Fatalf("findings = %v, want exactly [%s]", res.Attempts[0].Findings, expectedLadderMissingVerificationFinding)
	}
	// The touch-up step runs on the SAME planner (no escalation while
	// touch-up budget remains, no replan either).
	for _, c := range calls {
		if c.backend != ladderPlannerA || c.model != ladderPlannerAModel {
			t.Fatalf("escalated on the touch-up step: call %s/%s", c.backend, c.model)
		}
	}
	// The touch-up prompt names the flagged task and its findings, and
	// instructs a targeted fix of ONLY that task.
	touchUpPrompt := calls[1].prompt
	for _, want := range []string{
		"TOUCH-UP",
		"ONLY",
		"Record the planner chain on every attempt", // the flagged t2 title
		"verification_script",                       // the finding text
	} {
		if !strings.Contains(touchUpPrompt, want) {
			t.Fatalf("touch-up prompt missing %q:\n%s", want, touchUpPrompt)
		}
	}
	if strings.Contains(touchUpPrompt, "Track remaining touch-up and replay budgets") {
		t.Fatalf("touch-up prompt must not name the UNflagged task t1:\n%s", touchUpPrompt)
	}
	// The fixed plan is accepted; the executor kept t1 byte-identical and
	// only t2 moved.
	if len(res.FinalStories) != 1 {
		t.Fatalf("FinalStories = %+v", res.FinalStories)
	}
	t2, ok := taskByID(res.FinalStories, "t2")
	if !ok || !strings.Contains(t2.Spec, "fixed attempt record") {
		t.Fatalf("touch-up result must be the fixed t2: %+v (ok=%v)", t2, ok)
	}
	if t2.VerificationScriptReason == "" {
		t.Fatalf("t2 must carry its verification exemption: %+v", t2)
	}
	if res.Planner != ladderRefA {
		t.Fatalf("Planner = %+v, want %+v", res.Planner, ladderRefA)
	}
}

func TestPlanningRoutingTouchUpExhaustedReplans(t *testing.T) {
	// Single planner: with defaults (TouchUpMax 1, ReplanMax 1,
	// EscalateAfterAttempts 2) the ladder must spend its one touch-up,
	// then its one replay — not give up and not (unable to) escalate.
	flakyPlan := func(ctx context.Context, backend, model, prompt string) (string, error) {
		if strings.Contains(prompt, "REPLAN") {
			return ladderCleanPlanJSON, nil
		}
		return ladderFlaggedPlanJSON, nil
	}
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA}, flakyPlan)

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	expectKinds(t, res, LadderAttemptInitial, LadderAttemptTouchUp, LadderAttemptReplan)
	if res.Attempts[2].Outcome != LadderOutcomePass {
		t.Fatalf("replan outcome = %q, want pass", res.Attempts[2].Outcome)
	}
	// The replan prompt carries the findings as feedback — a fresh re-plan
	// of the whole spec, not another targeted fix.
	replanPrompt := calls[2].prompt
	for _, want := range []string{"REPLAN", "verification_script"} {
		if !strings.Contains(replanPrompt, want) {
			t.Fatalf("replan prompt missing %q:\n%s", want, replanPrompt)
		}
	}
	if res.Planner != ladderRefA {
		t.Fatalf("Planner = %+v, want %+v", res.Planner, ladderRefA)
	}
}

func TestPlanningRoutingEscalatesToSecondPlanner(t *testing.T) {
	// planner A fails every allowed attempt (default allowance 2, one of
	// them a touch-up); planner B passes on its fresh initial.
	plan := func(ctx context.Context, backend, model, prompt string) (string, error) {
		if backend == ladderPlannerA {
			return ladderFlaggedPlanJSON, nil
		}
		return ladderCleanPlanJSON, nil
	}
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA, ladderRefB}, plan)

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	expectKinds(t, res, LadderAttemptInitial, LadderAttemptTouchUp, LadderAttemptInitial)
	// The ladder spent planner A's attempts WITHIN the allowance, then
	// started fresh at planner B.
	if calls[0].backend != ladderPlannerA || calls[1].backend != ladderPlannerA {
		t.Fatalf("attempts 1-2 must run on planner A: %+v", calls)
	}
	if calls[2].backend != ladderPlannerB || calls[2].model != ladderPlannerBModel {
		t.Fatalf("attempt 3 must run on planner B: %+v", calls)
	}
	// Fresh start: planner B's initial prompt is the bare spec — no
	// findings fed over from planner A's failed plan.
	if calls[2].prompt != ladderTestSpec {
		t.Fatalf("escalation must restart fresh from the spec, got:\n%s", calls[2].prompt)
	}
	if res.Attempts[2].FindingCount != 0 {
		t.Fatalf("planner B attempt findings = %v, want none", res.Attempts[2].Findings)
	}
	if !reflect.DeepEqual(res.Planner, ladderRefB) {
		t.Fatalf("Planner = %+v, want the second planner %+v", res.Planner, ladderRefB)
	}
}

func TestPlanningRoutingExhaustsAllPlanners(t *testing.T) {
	// Both planners fail every attempt; every budget is ultimately spent.
	// Default budgets: 1 touch-up + 1 replay, shared across the chain.
	alwaysFlagged := func(ctx context.Context, backend, model, prompt string) (string, error) {
		return ladderFlaggedPlanJSON, nil
	}
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA, ladderRefB}, alwaysFlagged)

	if res.Passed {
		t.Fatalf("Passed = true: the ladder must exhaust, not pass: %+v", res)
	}
	// A: initial → A: touch-up (allowance 2 met) → B: initial → B: replan
	// (last shared budget) → out of move.
	expectKinds(t, res,
		LadderAttemptInitial, LadderAttemptTouchUp,
		LadderAttemptInitial, LadderAttemptReplan)
	backends := []string{calls[0].backend, calls[1].backend, calls[2].backend, calls[3].backend}
	wantBackends := []string{ladderPlannerA, ladderPlannerA, ladderPlannerB, ladderPlannerB}
	if !reflect.DeepEqual(backends, wantBackends) {
		t.Fatalf("attempt planners = %v, want %v", backends, wantBackends)
	}
	for i, a := range res.Attempts {
		if a.Outcome != LadderOutcomeFindings {
			t.Fatalf("attempt %d outcome = %q, want findings", i+1, a.Outcome)
		}
		if a.FindingCount != 1 {
			t.Fatalf("attempt %d FindingCount = %d (findings %v), want 1", i+1, a.FindingCount, a.Findings)
		}
	}
	// Terminal state: the last planner, and the last plan it holds.
	if !reflect.DeepEqual(res.Planner, ladderRefB) {
		t.Fatalf("Planner = %+v, want the last planner %#v", res.Planner, ladderRefB)
	}
	if len(res.FinalStories) != 1 {
		t.Fatalf("FinalStories = %+v, want the last plan it holds", res.FinalStories)
	}
	t2, _ := taskByID(res.FinalStories, "t2")
	if t2.VerificationScriptReason != "" {
		t.Fatalf("the held plan is still the flagged one: t2 = %+v", t2)
	}
}

func TestPlanningRoutingPlannerErrorEscalates(t *testing.T) {
	// planner A's call errors; a failed call has no plan to touch up, so
	// the ladder escalates to B (which parses clean) and records the
	// error on A's attempt.
	plan := func(ctx context.Context, backend, model, prompt string) (string, error) {
		if backend == ladderPlannerA {
			return "", errors.New("backend down")
		}
		return ladderCleanPlanJSON, nil
	}
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA, ladderRefB}, plan)

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	expectKinds(t, res, LadderAttemptInitial, LadderAttemptInitial)
	a1 := res.Attempts[0]
	if a1.Outcome != LadderOutcomePlannerError {
		t.Fatalf("attempt 1 outcome = %q, want planner_error", a1.Outcome)
	}
	if !strings.Contains(a1.Error, "backend down") {
		t.Fatalf("attempt 1 Error = %q, want the planner's message", a1.Error)
	}
	if a1.FindingCount != 0 || a1.Findings != nil {
		t.Fatalf("error attempt must carry no findings: %+v", a1)
	}
	if calls[0].backend != ladderPlannerA || calls[1].backend != ladderPlannerB {
		t.Fatalf("calls = %+v, want A (error) then B (fresh)", calls)
	}
	if !reflect.DeepEqual(res.Planner, ladderRefB) {
		t.Fatalf("Planner = %+v, want %+v", res.Planner, ladderRefB)
	}
}

func TestPlanningRoutingParseFailureReplans(t *testing.T) {
	// Single planner: a non-parsing raw output fails the attempt as
	// parse_failure; with no next planner the ladder spends its replay
	// budget on a fresh full prompt, which then passes.
	plan := func(ctx context.Context, backend, model, prompt string) (string, error) {
		if strings.Contains(prompt, "REPLAN") {
			return ladderCleanPlanJSON, nil
		}
		return "this is not a plan at all", nil
	}
	res, calls := runLadder(t, config.PlanningLadder{}, []config.PlannerRef{ladderRefA}, plan)

	if !res.Passed {
		t.Fatalf("Passed = false: %+v", res)
	}
	expectKinds(t, res, LadderAttemptInitial, LadderAttemptReplan)
	a1 := res.Attempts[0]
	if a1.Outcome != LadderOutcomeParseFailure {
		t.Fatalf("attempt 1 outcome = %q, want parse_failure", a1.Outcome)
	}
	if !strings.Contains(a1.Error, "parse") {
		t.Fatalf("attempt 1 Error = %q, want the parse message", a1.Error)
	}
	// A parse failure carries no plan, so no findings — the replan prompt
	// still explains WHAT failed is impossible; it only re-plans the spec.
	if a1.FindingCount != 0 || a1.Findings != nil {
		t.Fatalf("parse-failure attempt must carry no findings: %+v", a1)
	}
	if strings.Contains(calls[1].prompt, "TOUCH-UP") {
		t.Fatalf("a parse failure must not lead to a touch-up (no plan to touch):\n%s", calls[1].prompt)
	}
	if !strings.Contains(calls[1].prompt, "REPLAN") {
		t.Fatalf("after a parse failure at the end of the chain, want a replan:\n%s", calls[1].prompt)
	}
}

// expectedLadderMissingVerificationFinding — the exact structural
// finding the flagged plan must draw (structural rule 4 on t2), so the
// "exactly one finding" assertions are not vacuous.
const expectedLadderMissingVerificationFinding = "task t2 has neither verification_script nor verification_script_reason; per B124 a task without a script must state an explicit exemption reason"
