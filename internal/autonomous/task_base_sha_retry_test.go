package autonomous

import (
	"context"
	"testing"
	"time"
)

// TestBaseSHA_RetryDoesNotAdoptRetryHeadWhenFirstAttemptHadNoHead is a failing
// reproduction for a retry-time anchor corruption.
//
// Root cause (confirmed by reading the code): internal/autonomous/executor.go
// ~1091-1092 assigns the per-task anchor with
//
//	if t.BaseSHA == "" && sr.PreTaskSHA != "" {
//	    t.BaseSHA = sr.PreTaskSHA
//	}
//
// The guard only checks that BaseSHA is still empty, not that this is the
// task's FIRST attempt. When attempt 1's capture yields no SHA (for example a
// project dir with no commits yet: `git rev-parse HEAD` fails, so
// capturePreTask in cmd/datawatch/main.go ~4412-4450 leaves preTaskSHA ""),
// BaseSHA stays empty, and the first retry that does capture a SHA adopts its
// own pre-task HEAD, which already includes attempt 1's commit. The Task.BaseSHA
// contract (models.go ~676-680: "the git HEAD captured at the start of the
// task's FIRST attempt ... never overwritten by retries") is then broken.
//
// Expected: with no HEAD at the first attempt's start there is no anchor, so
// BaseSHA stays empty after the retry; PreTaskSHA tracks the retry's HEAD.
func TestBaseSHA_RetryDoesNotAdoptRetryHeadWhenFirstAttemptHadNoHead(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, Config{Enabled: true, AutoFixRetries: 1}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/proj", "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "do it"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	// Attempt 1 starts in a repo with no commits: no HEAD to capture.
	// Attempt 2 starts after attempt 1 committed, so it captures a real HEAD.
	const retryHeadSHA = "bbb222retryhead"
	verifyCalls := 0
	if err := m.Run(context.Background(), prd.ID,
		spawnSequence("", retryHeadSHA),
		func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
			verifyCalls++
			if verifyCalls == 1 {
				return VerificationResult{OK: false, Summary: "not yet done", VerifiedAt: time.Now()}, nil
			}
			return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
		},
	); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if verifyCalls != 2 {
		t.Fatalf("expected 2 verification attempts (one failed + one auto-retry), got %d", verifyCalls)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.BaseSHA != "" {
		t.Errorf("BaseSHA = %q, want empty (first attempt captured no HEAD, so a retry must not invent the anchor)", tk.BaseSHA)
	}
	if tk.PreTaskSHA != retryHeadSHA {
		t.Errorf("PreTaskSHA = %q, want %q (must track the latest attempt)", tk.PreTaskSHA, retryHeadSHA)
	}
}
