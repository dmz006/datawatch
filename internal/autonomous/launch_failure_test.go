// Launch-failure retry coverage: a verify() error from LaunchFailureError
// (session ended with no task delivery) must retry through the stall branch of
// the executor rather than being treated as finished work.
package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLaunchFailureError_WrapsWorkerStalled(t *testing.T) {
	err := LaunchFailureError("s1", "claude-code", "killed")
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("LaunchFailureError must wrap ErrWorkerStalled: %v", err)
	}
	if !strings.Contains(err.Error(), LaunchFailureReason) {
		t.Fatalf("missing reason in %q", err)
	}
}

func TestLaunchFailureRetry_RetriesThenRecovers(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})

	spawns := 0
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	calls := 0
	var hint string
	verify := func(_ context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
		calls++
		if calls == 1 {
			return VerificationResult{}, LaunchFailureError("s-1", "claude-code", "killed")
		}
		hint = tk.Error
		return VerificationResult{OK: true, Summary: "good"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("launch failure that recovers must complete, got %s", got.Status)
	}
	if calls != 2 || spawns != 2 {
		t.Fatalf("want 2 verify calls and 2 spawns, got %d/%d", calls, spawns)
	}
	if !strings.Contains(hint, LaunchFailureReason) {
		t.Fatalf("retry hint should carry launch-failure reason, got %q", hint)
	}
}

func TestLaunchFailureRetry_BoundedAndFailsInfra(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{}, LaunchFailureError("s-1", "claude-code", "killed")
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDFailed {
		t.Fatalf("persistent launch failure must fail once the budget is spent, got %s", got.Status)
	}
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskFailed {
		t.Fatalf("task must end failed, got %s", tk.Status)
	}
	if !strings.Contains(tk.Error, LaunchFailureReason) {
		t.Fatalf("task Error should contain launch-failure reason, got %q", tk.Error)
	}
}
