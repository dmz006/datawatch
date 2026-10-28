package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const attemptLimitEvidenceReason = "no progress for 1800 seconds: no output, no file changes, no commits"

func TestAttemptLimit_ErrorIsWorkerStalled(t *testing.T) {
	err := AttemptLimitError(attemptLimitEvidenceReason, 3600, 1800)
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("errors.Is(AttemptLimitError, ErrWorkerStalled) = false; err = %v", err)
	}
}

func TestAttemptLimit_ErrorText(t *testing.T) {
	err := AttemptLimitError(attemptLimitEvidenceReason, 3600, 1800)
	got := err.Error()
	if !strings.Contains(got, attemptLimitEvidenceReason) {
		t.Errorf("error text %q does not contain the reason verbatim", got)
	}
	for _, want := range []string{
		"task attempt stopped:",
		"task_max_attempt_seconds=3600s",
		"task_no_progress_seconds=1800s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error text %q does not contain %q", got, want)
		}
	}
}

// An attempt-limit stop (ErrWorkerStalled via AttemptLimitError) must
// consume the normal auto_fix_retries budget like any other stall and
// still fail the task once retries are exhausted, with the evidence
// reason recorded on the task.
func TestExecutor_AttemptLimitExhaustsRetriesLikeNormalFailure(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDApproved
	_ = m.Store().SavePRD(p)

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{}, AttemptLimitError(attemptLimitEvidenceReason, 3600, 1800)
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDFailed {
		t.Fatalf("repeated attempt-limit stops must fail once retries are exhausted, got %s", got.Status)
	}
	taskErr := got.Story[0].Tasks[0].Error
	if !strings.Contains(taskErr, attemptLimitEvidenceReason) {
		t.Errorf("task error %q does not contain the attempt-limit evidence reason", taskErr)
	}
}
