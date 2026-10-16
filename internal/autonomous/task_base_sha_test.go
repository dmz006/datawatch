package autonomous

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// spawnSequence returns a SpawnFn that hands out pre-task SHAs in order
// (last one repeats) so a task that spawns twice sees two different SHAs.
func spawnSequence(shas ...string) SpawnFn {
	i := 0
	return func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		sha := shas[len(shas)-1]
		if i < len(shas) {
			sha = shas[i]
			i++
		}
		return SpawnResult{SessionID: "sess-" + strconv.Itoa(i), PreTaskSHA: sha}, nil
	}
}

// TestBaseSHA_AnchorsFirstAttemptAcrossAutoRetries spawns the same task
// twice — the first spawn's verification fails, the auto-retry spawns a
// second time with a different pre-task SHA. BaseSHA must keep the FIRST
// spawn's SHA (the per-task anchor, never overwritten by retries) while
// PreTaskSHA tracks the latest attempt.
func TestBaseSHA_AnchorsFirstAttemptAcrossAutoRetries(t *testing.T) {
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

	const firstSHA, secondSHA = "aaa111firstsha", "bbb222secondsha"
	verifyCalls := 0
	if err := m.Run(context.Background(), prd.ID,
		spawnSequence(firstSHA, secondSHA),
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
	if tk.BaseSHA != firstSHA {
		t.Errorf("BaseSHA = %q, want %q (first attempt's anchor must survive the auto-retry)", tk.BaseSHA, firstSHA)
	}
	if tk.PreTaskSHA != secondSHA {
		t.Errorf("PreTaskSHA = %q, want %q (must track the latest attempt)", tk.PreTaskSHA, secondSHA)
	}
}

// TestResetTask_ClearsBaseSHAAndAttemptCommitSHAs verifies that a manual
// reset (operator ResetTask of a failed task) starts the task over: both
// the per-task SHA anchor and the accumulated per-attempt commit SHAs are
// cleared, so the retry re-captures them from scratch.
func TestResetTask_ClearsBaseSHAAndAttemptCommitSHAs(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		ID: "story-1", Title: "Story One",
		Tasks: []Task{{ID: "task-1a", Title: "Task 1A"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.Story[0].Status = StoryFailed
	tk := &prd.Story[0].Tasks[0]
	tk.Status = TaskFailed
	tk.Error = "attempt failed"
	tk.BaseSHA = "aaa111firstsha"
	tk.AttemptCommitSHAs = []string{"commit-a", "commit-b"}
	_ = m.Store().SavePRD(prd)

	if _, err := m.ResetTask(prd.ID, "task-1a", "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	gotTk := got.Story[0].Tasks[0]
	if gotTk.BaseSHA != "" {
		t.Errorf("after manual reset BaseSHA = %q, want empty (start over)", gotTk.BaseSHA)
	}
	if len(gotTk.AttemptCommitSHAs) != 0 {
		t.Errorf("after manual reset AttemptCommitSHAs = %v, want none (start over)", gotTk.AttemptCommitSHAs)
	}
	if gotTk.Status != "" && gotTk.Status != TaskPending {
		t.Errorf("after manual reset status = %q, want pending", gotTk.Status)
	}
}

// TestResetTask_Force_ClearsBaseSHAAndAttemptCommitSHAs covers the
// force=true path (requeue of a completed task) — same start-over rule.
func TestResetTask_Force_ClearsBaseSHAAndAttemptCommitSHAs(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		ID: "story-1", Title: "Story One",
		Tasks: []Task{{ID: "task-1a", Title: "Task 1A"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.Story[0].Status = StoryCompleted
	tk := &prd.Story[0].Tasks[0]
	tk.Status = TaskCompleted
	tk.BaseSHA = "ccc333basesha"
	tk.AttemptCommitSHAs = []string{"commit-done"}
	_ = m.Store().SavePRD(prd)

	if _, err := m.ResetTask(prd.ID, "task-1a", "operator", true); err != nil {
		t.Fatalf("ResetTask(force): %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	gotTk := got.Story[0].Tasks[0]
	if gotTk.BaseSHA != "" {
		t.Errorf("after force reset BaseSHA = %q, want empty (start over)", gotTk.BaseSHA)
	}
	if len(gotTk.AttemptCommitSHAs) != 0 {
		t.Errorf("after force reset AttemptCommitSHAs = %v, want none (start over)", gotTk.AttemptCommitSHAs)
	}
}
