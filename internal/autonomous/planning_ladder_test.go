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

// planning_ladder_test.go — TestPlanningRouting* runs the planning
// ladder state machine against scripted raw plans (PlannerFn stubs);
// no LLM is called anywhere here. The real parse (ParsePlanning), the
// three real deterministic finders (LintPlanScope, CheckPlanCoverage,
// CheckPlanStructure) and the real budgets (config.PlanningLadder)
// run unmodified.

const (
	ladderTestSpec = "Cover requirement A.\nCover requirement B."
)

// ladderFlaggedPlanJSON covers both spec requirements in t1 but leaves
// t2 without a verification command or the exemption — every planner
// that returns it gets the findings, so the ladder keeps stepping on it.
const ladderFlaggedPlanJSON = `{
  "title": "planned",
  "stories": [
    {
      "id": "s1",
      "title": "Build",
      "tasks": [
        { "id": "t1", "title": "t1", "spec": "Cover requirement A\nverification_command: echo a", "files_planned": ["docs/a.md"], "verification_script": "tests/a.sh" },
        { "id": "t2", "title": "t2", "spec": "Cover requirement B" }
      ]
    }
  ]
}`

// ladderFixedPlanJSON is the ladder's accepting answer: t2 gains the
// verification exemption (B124) so the three checks pass.
const ladderFixedPlanJSON = `{
  "title": "planned",
  "stories": [
    {
      "id": "s1",
      "title": "Build",
      "tasks": [
        { "id": "t1", "title": "t1", "spec": "Cover requirement A\nverification_command: echo a", "files_planned": ["docs/a.md"], "verification_script": "tests/a.sh" },
        { "id": "t2", "title": "t2", "spec": "Cover requirement B", "verification_script": "tests/b.sh" }
      ]
    }
  ]
}`

// ladderSneakyFixedPlanJSON is what a NON-honoring planner returns for
// a touch-up: it fixes t2 (as it must) but also tampers with the
// unflagged t1 (different spec text) and adds a third task. The ladder
// must accept only the t2 replacement and hold everything else in place
// — that is what the TouchUpFixesOnlyFlaggedTask test asserts.
const ladderSneakyFixedPlanJSON = `{
  "title": "planned",
  "stories": [
    {
      "id": "s1",
      "title": "Build",
      "tasks": [
        { "id": "t1", "title": "t1 (rewritten)", "spec": "TAMPERED with requirement A" },
        { "id": "t2", "title": "t2", "spec": "Cover requirement B", "verification_script": "tests/b.sh" },
        { "id": "t3", "title": "t3", "spec": "added work nobody asked for\nverification_command: echo 3", "verification_script": "tests/c.sh" }
      ]
    }
  ]
}`

// --- helpers ------------------------------------------------------------------

type ladderCall struct {
	backend, model, prompt string
}

// newScriptedPlanner returns a PlannerFn that replies with raw[i] for
// the i-th call recorded in calls (any backend/model), or err[i] when
// raw[i] == "" and err[i] != nil.
func newScriptedPlanner(t *testing.T, calls *[]ladderCall, raw []string, err []error) PlannerFn {
	t.Helper()
	planIx := 0
	return func(ctx context.Context, backend, model, prompt string) (string, error) {
		i := len(*calls)
		*calls = append(*calls, ladderCall{backend, model, prompt})
		if i < len(err) && err[i] != nil {
			return "", err[i]
		}
		if planIx < len(raw) {
			p := raw[planIx]
			planIx++
			return p, nil
		}
		t.Fatalf("planner called more than scripted (%d)", i)
		panic("unreachable")
	}
}

// expectKinds asserts the attempt kinds of the whole ladder run in order.
func expectKinds(t *testing.T, res LadderResult, want []string) {
	t.Helper()
	got := make([]string, len(res.Attempts))
	for i, a := range res.Attempts {
		got[i] = a.Kind
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attempts got %v want %v", got, want)
	}
}

func ladderFindTask(stories []Story, taskID string) (Task, bool) {
	for _, s := range stories {
		for _, t := range s.Tasks {
			if t.ID == taskID {
				return t, true
			}
		}
	}
	return Task{}, false
}

// ladderDefaultClock advances one second per read so every attempt's
// clock is distinct and comparable.
func ladderDeterministicClock(start time.Time) func() time.Time {
	n := 0
	return func() time.Time { return start.Add(time.Duration(n) * time.Second) }
}

// --- the run ladder -----------------------------------------------------------

var ladderDefaultBench = config.PlanningLadder{} // all budgets at the defaults 1 / 1 / 2

// TestPlanningRoutingPassesOnFirstAttempt pins the happy path: a clean
// plan ends the ladder after one attempt, attributed to the first
// planner.
func TestPlanningRoutingPassesOnFirstAttempt(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		newScriptedPlanner(t, &calls, []string{ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed {
		t.Fatalf("ladder must pass on a clean plan, attempts=%+v", res.Attempts)
	}
	if res.Planner != (config.PlannerRef{Backend: "a", Model: "m1"}) {
		t.Fatalf("pass must be attributed to the first planner, got %+v", res.Planner)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("pass must carry no findings, got %v", res.Findings)
	}
	expectKinds(t, res, []string{LadderAttemptPlan})
	if res.Attempts[0].Outcome != LadderOutcomePassed {
		t.Fatalf("clean attempt outcome want passed got %q", res.Attempts[0].Outcome)
	}

	// First move: the bare spec, nothing yet to feed back.
	if calls[0].prompt != ladderTestSpec {
		t.Fatalf("initial prompt must be the bare spec, got %q", calls[0].prompt)
	}
	if len(calls) != 1 {
		t.Fatalf("a passing plan must spend no budget: got %d planner calls", len(calls))
	}
}

// TestPlanningRoutingTouchUpFixesOnlyFlaggedTask pins the touch-up
// contract end to end: one flagged task (t2, no B124 verification
// exemption), the touch-up prompt names the flagged title + the finding
// (and not the unflagged task), and the planner answers with a fixed
// full plan that — as planners do — also tampers with unflagged work.
// The ladder must still end with EXACTLY: t2 fixed, t1 preserved
// byte-for-byte, no added task.
func TestPlanningRoutingTouchUpFixesOnlyFlaggedTask(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		newScriptedPlanner(t, &calls, []string{ladderFlaggedPlanJSON, ladderSneakyFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed {
		t.Fatalf("touch-up must end the ladder, attempts=%+v", res.Attempts)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptTouchUp})
	if res.Attempts[1].Outcome != LadderOutcomePassed {
		t.Fatalf("touch-up outcome want passed got %q", res.Attempts[1].Outcome)
	}

	// The first attempt's failure: the finding names the flagged task
	// title and the B124 exemption, and nothing names t1.
	first := res.Attempts[0]
	if first.Kind != LadderAttemptPlan || first.Outcome != LadderOutcomeFailed || first.FindingCount != len(first.Findings) {
		t.Fatalf("first attempt shape: %+v", first)
	}
	if !strings.Contains(first.Findings[0], "t2") || !strings.Contains(first.Findings[0], "B124") {
		t.Fatalf("finding must flag t2's missing B124 exemption, got %q", first.Findings[0])
	}

	// The touch-up prompt carries the spec, the flagged title, and NOT
	// the unflagged task as a flagged entry; it does not carry the
	// replan shape, and the plan prompt did not carry the touch-up
	// findings.
	p1, p2 := calls[0].prompt, calls[1].prompt
	if p1 != ladderTestSpec {
		t.Fatalf("first attempt must be the bare spec, got %q", p1)
	}
	if !strings.Contains(p2, "TOUCH-UP") {
		t.Fatalf("second attempt must be a touch-up, got %q", p2)
	}
	if !strings.Contains(p2, "t2") || strings.Contains(p2, "t1") {
		t.Fatalf("touch-up must name the flagged task (t2) and not the other (t1): %q", p2)
	}
	if !strings.Contains(p2, first.Findings[0]) {
		t.Fatalf("touch-up must feed the flagged finding back, prompt=%q finding=%q", p2, first.Findings[0])
	}

	// The merge: final stories hold t1 BYTE-IDENTICAL to the plan the
	// failed attempt held (the sneaky response's rewritten t1 and its
	// added t3 must not have moved), and t2 exactly as the response
	// carried it.
	if len(res.FinalStories) != 1 || len(res.FinalStories[0].Tasks) != 2 {
		t.Fatalf("final plan must be the base plan with t2 replaced, got %+v", res.FinalStories)
	}
	var baseStories []Story
	_, baseStories, err := ParsePlanning(ladderFlaggedPlanJSON)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	baseT1, ok := ladderFindTask(baseStories, "t1")
	if !ok {
		t.Fatalf("fixture has no t1")
	}
	gotT1, ok := ladderFindTask(res.FinalStories, "t1")
	if !ok {
		t.Fatalf("final plan dropped the unflagged task t1")
	}
	if !reflect.DeepEqual(gotT1, baseT1) {
		t.Fatalf("unflagged task t1 must stay byte-identical to the last plan\ngot:  %+v\nwant: %+v", gotT1, baseT1)
	}
	_, updatedStories, err := ParsePlanning(ladderSneakyFixedPlanJSON)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	wantT2, ok := ladderFindTask(updatedStories, "t2")
	if !ok {
		t.Fatalf("fixture has no t2")
	}
	gotT2, ok := ladderFindTask(res.FinalStories, "t2")
	if !ok {
		t.Fatalf("final plan dropped the flagged task t2")
	}
	if !reflect.DeepEqual(gotT2, wantT2) {
		t.Fatalf("flagged task t2 must be the response's replacement\ngot:  %+v\nwant: %+v", gotT2, wantT2)
	}
	if _, added := ladderFindTask(res.FinalStories, "t3"); added {
		t.Fatalf("unflagged work in the touch-up response must not be added")
	}
}

// TestPlanningRoutingTouchUpExhaustedReplans pins the budget order:
// with the touch-up budget spent, the run ladder is carried to the
// replay budget at the same planner. Touch-ups never count against the
// per-planner allowance, so the single planner here gets both its
// budgets before the ladder is out of move.
func TestPlanningRoutingTouchUpExhaustedReplans(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		newScriptedPlanner(t, &calls,
			[]string{ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed {
		t.Fatalf("the replayed plan must carry the ladder, attempts=%+v", res.Attempts)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptTouchUp, LadderAttemptReplan})
	if len(calls) != 3 {
		t.Fatalf("one touch-up + one replay = three planner calls, got %d", len(calls))
	}
	for i, c := range calls {
		if !strings.Contains(c.prompt, ladderTestSpec) {
			t.Fatalf("attempts %d must carry the spec, got %q", i+1, c.prompt)
		}
	}
	// The replayed prompt carries the findings as feedback; the touch-up
	// prompt named the flagged task instead.
	if !strings.Contains(calls[2].prompt, "REPLAN") || !strings.Contains(calls[2].prompt, res.Attempts[0].Findings[0]) {
		t.Fatalf("replan must feed the failed plan's findings, prompt=%q", calls[2].prompt)
	}
	if res.Planner != (config.PlannerRef{Backend: "a", Model: "m1"}) {
		t.Fatalf("a single-chain ladder keeps the pass at this planner, got %+v", res.Planner)
	}
}

// TestPlanningRoutingEscalatesToSecondPlanner pins the allowance: once
// the first planner has burned its two PLAN attempts (its plan and its
// replay; its one touch-up did not count), the run ladder restarts at
// the second planner with a fresh plan — the bare spec, no findings
// carried over — and spending only whatever budget is left (none here).
func TestPlanningRoutingEscalatesToSecondPlanner(t *testing.T) {
	var calls []ladderCall
	// A never accepts: whatever it answers, it flags.
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{
			{Backend: "a", Model: "m1"},
			{Backend: "b", Model: "m2"},
		},
		ladderTestSpec,
		newScriptedPlanner(t, &calls,
			[]string{ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed || res.Planner != (config.PlannerRef{Backend: "b", Model: "m2"}) {
		t.Fatalf("planner B must earn the pass, result=%+v attempts=%+v", res, res.Attempts)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptTouchUp, LadderAttemptReplan, LadderAttemptPlan})

	// A's three moves, then B's fresh plan.
	if calls[0].backend != "a" || calls[1].backend != "a" || calls[2].backend != "a" || calls[3].backend != "b" {
		t.Fatalf("backend order wrong: %v", []string{calls[0].backend, calls[1].backend, calls[2].backend, calls[3].backend})
	}
	if calls[3].prompt != ladderTestSpec {
		t.Fatalf("escalation must restart from the bare spec, got %q", calls[3].prompt)
	}
	if strings.Contains(calls[3].prompt, "B124") {
		t.Fatalf("escalation must not carry the previous planner's findings, got %q", calls[3].prompt)
	}
}

// TestPlanningRoutingExhaustsAllPlanners pins the terminal state: both
// planners fail every move they get, the shared budgets are spent, and
// the ladder is out of move: Passed=false, the LAST plan (from the
// last planner) held, the findings of that plan, every attempt with
// the planner they ran on, and no further call.
func TestPlanningRoutingExhaustsAllPlanners(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{
			{Backend: "a", Model: "m1"},
			{Backend: "b", Model: "m2"},
		},
		ladderTestSpec,
		// Every planner answers the flagged plan: the ladder's only
		// path out is the one it is designed to walk.
		newScriptedPlanner(t, &calls,
			[]string{ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if res.Passed {
		t.Fatalf("all planners failed, the ladder must not pass")
	}
	if len(res.Attempts) != 4 || len(calls) != 4 {
		t.Fatalf("default budgets: three moves at A (plan, touch-up, replay) then A's plan at B = 4 attempts, got attempts=%d calls=%d", len(res.Attempts), len(calls))
	}
	wantBackends := []string{"a", "a", "a", "b"}
	for i, a := range res.Attempts {
		if a.Backend != wantBackends[i] {
			t.Fatalf("attempt %d backend want %s got %s (%+v)", i+1, wantBackends[i], a.Backend, a)
		}
		if a.Outcome != LadderOutcomeFailed || a.FindingCount == 0 {
			t.Fatalf("each failed attempt records the findings it saw: %+v", a)
		}
		if a.Error != "" {
			t.Fatalf("these attempts parsed fine, no error expected: %+v", a)
		}
	}
	if !reflect.DeepEqual(res.FinalStories, nil) && len(res.FinalStories) == 0 {
		t.Fatalf("the failed ladder still holds the last plan")
	}
	if got, want := res.Planner, (config.PlannerRef{Backend: "b", Model: "m2"}); got != want {
		t.Fatalf("the held plan is planner B's, attribution want %+v got %+v", want, got)
	}
	// The result carries the last findings the ladder saw — the caller
	// should not have to dig through the attempts.
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0], "B124") {
		t.Fatalf("result.Findings must carry the last findings, got %v", res.Findings)
	}
	if len(calls) != 4 {
		t.Fatalf("out of move: the ladder stops calling, got %d calls", len(calls))
	}
}

// TestPlanningRoutingPlannerErrorEscalates pins an error in the middle
// of the chain: the error attempt is recorded (the ladder does not
// retry the same broken planner), and the run ladder moves on to the
// next planner.
func TestPlanningRoutingPlannerErrorEscalates(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{
			{Backend: "a", Model: "m1"},
			{Backend: "b", Model: "m2"},
		},
		ladderTestSpec,
		newScriptedPlanner(t, &calls,
			[]string{ladderFixedPlanJSON},
			[]error{errors.New("planner a is down"), nil}),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed || res.Planner != (config.PlannerRef{Backend: "b", Model: "m2"}) {
		t.Fatalf("the next planner must earn the pass after a planner error, got %+v attempts=%+v", res, res.Attempts)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptPlan})
	errRec := res.Attempts[0]
	if errRec.Outcome != LadderOutcomeError {
		t.Fatalf("a planner error records outcome error, got %q", errRec.Outcome)
	}
	if !strings.Contains(errRec.Error, "planner a is down") {
		t.Fatalf("the error message belongs on the record: %+v", errRec)
	}
	if errRec.Findings != nil || errRec.FindingCount != 0 {
		t.Fatalf("no plan, no findings on an error attempt: %+v", errRec)
	}
	if res.Attempts[1].Outcome != LadderOutcomePassed {
		t.Fatalf("B's plan must pass, got %q", res.Attempts[1].Outcome)
	}
	if !strings.Contains(calls[1].prompt, ladderTestSpec) {
		t.Fatalf("escalation after an error restarts fresh from the spec, got %q", calls[1].prompt)
	}
}

// TestPlanningRoutingParseFailureReplans pins the parse-failure path
// at the end of the chain: the failed raw output is recorded (outcome
// failed, the parse error on the record, zero findings — the checks
// never ran on it), and the ladder spends its replay budget on a fresh
// plan at the same planner.
func TestPlanningRoutingParseFailureReplans(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(), ladderDefaultBench,
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		// First answer is not a plan the ladder can parse; the replayed
		// one is.
		newScriptedPlanner(t, &calls,
			[]string{"sure! here is the plan I would think about:\n(it is prose, not JSON)", ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed {
		t.Fatalf("the replayed answer must pass the ladder, attempts=%+v", res.Attempts)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptReplan})
	bad := res.Attempts[0]
	if bad.Outcome != LadderOutcomeFailed {
		t.Fatalf("a parse failure records outcome failed, got %q", bad.Outcome)
	}
	if !strings.Contains(bad.Error, "parse") {
		t.Fatalf("the parse error belongs on the record: %+v", bad)
	}
	if bad.Findings != nil || bad.FindingCount != 0 {
		t.Fatalf("a raw that never parses has zero findings, got %+v", bad)
	}
	if res.Attempts[1].Outcome != LadderOutcomePassed {
		t.Fatalf("the replayed plan must pass, got %q", res.Attempts[1].Outcome)
	}
	// A replay is a fresh plan: the prompt does not carry the touch-up
	// shape and still carries the spec.
	if !strings.Contains(calls[1].prompt, "REPLAN") || !strings.Contains(calls[1].prompt, ladderTestSpec) {
		t.Fatalf("replan prompt must re-plan the whole spec, got %q", calls[1].prompt)
	}
}

// TestPlanningRoutingGuards pins the two documented guards: nil
// planner function and empty planner chain both terminate the ladder
// with a clean not-passing result and zero calls / attempts — no panic,
// no invented plan.
func TestPlanningRoutingGuards(t *testing.T) {
	t.Run("nil_planner", func(t *testing.T) {
		res := RunPlanningLadder(context.Background(), ladderDefaultBench,
			[]config.PlannerRef{{Backend: "a", Model: "m1"}}, ladderTestSpec, nil,
			ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))
		if res.Passed || len(res.Attempts) != 0 || res.FinalStories != nil {
			t.Fatalf("nil planner ends the ladder without a plan, got %+v", res)
		}
	})
	t.Run("empty_chain", func(t *testing.T) {
		var calls []ladderCall
		res := RunPlanningLadder(context.Background(), ladderDefaultBench,
			nil, ladderTestSpec,
			newScriptedPlanner(t, &calls, []string{ladderFixedPlanJSON}, nil),
			ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))
		if res.Passed || len(calls) != 0 {
			t.Fatalf("an empty chain must not call at all, calls=%d res=%+v", len(calls), res)
		}
	})
}

// TestPlanningRoutingCancelledStops pins ctx handling: a context that
// is already cancelled stops the ladder after zero attempts, with
// everything the ladder already recorded — here, nothing.
func TestPlanningRoutingCancelledStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls []ladderCall
	res := RunPlanningLadder(ctx, ladderDefaultBench,
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		newScriptedPlanner(t, &calls, []string{ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))
	if res.Passed || len(res.Attempts) != 0 || len(calls) != 0 {
		t.Fatalf("a cancelled context ends the ladder at once, got res=%+v calls=%d", res, len(calls))
	}
}

// TestPlanningRoutingBudgetsAreShared pins the shared-budget rule
// with explicit (non-default) numbers: touch-up budget = 2, replay = 1,
// allowance = 3. A single planner that keeps answering the flagged
// plan spends: plan, touch-up, touch-up, replay — then, even though its
// plan-attempt allowance (3) has not been reached, there is no next
// planner and no budget left, so it is out of move on move five.
func TestPlanningRoutingBudgetsAreShared(t *testing.T) {
	var calls []ladderCall
	res := RunPlanningLadder(context.Background(),
		config.PlanningLadder{TouchUpMax: 2, ReplanMax: 1, EscalateAfterAttempts: 3},
		[]config.PlannerRef{{Backend: "a", Model: "m1"}},
		ladderTestSpec,
		newScriptedPlanner(t, &calls,
			[]string{ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if res.Passed {
		t.Fatalf("budgets spent at one planner must end the ladder failed")
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptTouchUp, LadderAttemptTouchUp, LadderAttemptReplan})
	if len(calls) != 4 {
		t.Fatalf("touch-up budget 2 + replay budget 1 + the plan = 4 calls, got %d", len(calls))
	}
}

// TestPlanningRoutingEscalatesByAllowance pins the allowance as the
// trigger, not the budgets: a 2-planner chain, escalation after 2 PLAN
// attempts, NO touch-up budget (0 = default 1, so set it explicitly to
// 1 here) and replay budget 1. A burns its two plan attempts (plan,
// replay — the touch-up in between does not count), the ladder
// escalates to B BEFORE B has spent anything, and B's plan is the fifth
// call in the run.
func TestPlanningRoutingEscalatesByAllowance(t *testing.T) {
	var calls []ladderCall
	chain := []config.PlannerRef{
		{Backend: "a", Model: "m1"},
		{Backend: "b", Model: "m2"},
	}
	res := RunPlanningLadder(context.Background(),
		config.PlanningLadder{TouchUpMax: 1, ReplanMax: 1, EscalateAfterAttempts: 2},
		chain,
		ladderTestSpec,
		// A flags three times (plan, touch-up, replay); B's plan passes.
		newScriptedPlanner(t, &calls,
			[]string{ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFlaggedPlanJSON, ladderFixedPlanJSON}, nil),
		ladderDeterministicClock(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)))

	if !res.Passed || res.Planner != chain[1] {
		t.Fatalf("B must earn the pass once A has burned its plan-attempt allowance, got %+v", res)
	}
	expectKinds(t, res, []string{LadderAttemptPlan, LadderAttemptTouchUp, LadderAttemptReplan, LadderAttemptPlan})
	if got := []string{calls[0].backend, calls[1].backend, calls[2].backend, calls[3].backend}; !reflect.DeepEqual(got, []string{"a", "a", "a", "b"}) {
		t.Fatalf("A takes its plan, its touch-up and its replay before B takes a single move, got %v", got)
	}
}
