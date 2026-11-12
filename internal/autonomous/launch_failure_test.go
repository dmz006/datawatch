// Launch-failure retry coverage: a verify() error from LaunchFailureError
// (session ended with no task delivery) must retry through the stall branch of
// the executor rather than being treated as finished work.
package autonomous

import (
	"context"
	"errors"
	"fmt"
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
	// A real existing project dir: with a fake nonexistent dir (/w/proj) a
	// boot-resume scan goroutine (launched by SetExecutors) can park a
	// finished PRD (dir-missing, B165 gate) and clobber the PRDCompleted state
	// asserted below — the race that made this test flaky in CI. t.TempDir()
	// is unique per call and auto-cleaned.
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
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
	// A real existing project dir: a fake nonexistent one (/w/proj) is
	// exactly what the boot-resume gate (B165) parks a PRD for, so it could
	// overwrite the PRDFailed terminal state this test asserts on — the CI
	// flake. A fresh real dir removes that input entirely.
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{}, LaunchFailureError("s-1", "claude-code", "killed")
	}
	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDBlocked {
		t.Fatalf("persistent launch failure must hold the task once the budget is spent, got %s", got.Status)
	}
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskReviewNeeded {
		t.Fatalf("task must be held for review, got %s", tk.Status)
	}
	// The held task's Error is the one-line hold note; the cause is in the failure detail.
	if tk.FailureReason == nil || !strings.Contains(tk.FailureReason.Detail, LaunchFailureReason) {
		t.Fatalf("failure detail should contain the launch-failure reason, got %+v", tk.FailureReason)
	}
}

// TestLaunchFailureRetry_BoundedAndFailsInfra_NeverBlocked is the
// regression guard for the CI race that made the launch-failure tests flaky:
// the boot-resume scan goroutine (launched by SetExecutors in the shared
// helper) could observe the just-finished PRD in a terminal state and park
// it PRDBlocked — clobbering the PRDFailed terminal state the tests assert —
// or, on the recover side, flip a PRDCompleted PRD back into a re-runnable
// state that the old 5s status poll then timed out on.
//
// 20 fresh fixtures, each exercising the full persistent-launch-failure path
// (budget of 2 retries spent on a LaunchFailureError from verify), asserting
// the PRD ALWAYS ends PRDFailed with the launch-failure reason in the task
// error and NEVER ends PRDBlocked. A 1-in-N park race produced PRDBlocked or
// a timeout on the old status-polling helpers; the drained-scan +
// executor-exit wait sequence (settlePRD) closes the window, and this loop is
// the cheap statistical proof it stays closed.
func TestLaunchFailureRetry_BoundedAndFailsInfra_NeverBlocked(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("iter-%d", i), func(t *testing.T) {
			m, api, _, _ := apiFixture(t)
			useInfraCountAction(m) // the legacy failed path, not a hold for review
			// A fresh real project dir per iteration: a fake nonexistent dir
			// is precisely what the boot-resume gate treats as "park this PRD",
			// so every iteration gets a new existing one (t.TempDir() is unique
			// per call and auto-cleaned at test end).
			prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
			_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
			// The race this loop pins belongs to the legacy terminal state; a held
			// Automaton is a legitimate PRDBlocked, so escalation stays off here.
			fp, _ := m.Store().GetPRD(prd.ID)
			fp.FailureEscalation = &FailureEscalationPolicy{Enabled: escalationOn(false)}
			_ = m.Store().SavePRD(fp)

			spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
				return SpawnResult{SessionID: "s-" + r.TaskID}, nil
			}
			verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
				return VerificationResult{}, LaunchFailureError("s-1", "claude-code", "killed")
			}
			got := runToTerminal(t, m, api, prd.ID, spawn, verify)
			if got.Status == PRDBlocked {
				t.Fatalf("PRD ended PRDBlocked — boot-resume scan raced the run (the CI flake)")
			}
			if got.Status != PRDFailed {
				t.Fatalf("want PRDFailed, got %s", got.Status)
			}
			tk := got.Story[0].Tasks[0]
			if !strings.Contains(tk.Error, LaunchFailureReason) {
				t.Fatalf("task Error should contain launch-failure reason, got %q", tk.Error)
			}
		})
	}
}
