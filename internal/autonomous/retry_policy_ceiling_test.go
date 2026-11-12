package autonomous

import (
	"strings"
	"testing"
)

func ceilingTask(attempts ...TaskAttempt) (*PRD, *Task) {
	t := &Task{ID: "t1", Title: "work", Status: TaskInProgress, Attempts: attempts}
	return &PRD{ID: "p1"}, t
}

func newCeilingManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCeilingOneFailureRetries(t *testing.T) {
	m := newCeilingManager(t)
	prd, task := ceilingTask(TaskAttempt{Number: 1, MemoryKilled: true})
	if held, err := m.holdAfterCeilingFailures(prd, task); held || err != nil {
		t.Fatalf("one ceiling failure: held=%v err=%v, want retry", held, err)
	}
	if task.Status == TaskReviewNeeded {
		t.Error("task moved to review after one ceiling failure")
	}
}

func TestCeilingTwoInARowGoesToReview(t *testing.T) {
	m := newCeilingManager(t)
	prd, task := ceilingTask(TaskAttempt{Number: 1, MemoryKilled: true}, TaskAttempt{Number: 2, MemoryKilled: true})
	held, err := m.holdAfterCeilingFailures(prd, task)
	if !held || err != nil {
		t.Fatalf("two ceiling failures: held=%v err=%v, want review", held, err)
	}
	if task.Status != TaskReviewNeeded {
		t.Errorf("status = %s, want review", task.Status)
	}
	if task.FailureReason == nil || task.FailureReason.Detail != AttemptMemoryKillReason || task.FailureReason.Retryable {
		t.Errorf("failure reason = %+v", task.FailureReason)
	}
	if task.ReviewSummary == nil || !strings.Contains(task.ReviewSummary.Text, AttemptMemoryKillReason) {
		t.Errorf("review summary lacks the reason: %+v", task.ReviewSummary)
	}
}

func TestCeilingNonCeilingFailureResetsCount(t *testing.T) {
	m := newCeilingManager(t)
	prd, task := ceilingTask(
		TaskAttempt{Number: 1, MemoryKilled: true},
		TaskAttempt{Number: 2, Outcome: "failed"},
		TaskAttempt{Number: 3, MemoryKilled: true},
	)
	if n := consecutiveCeilingFailures(task); n != 1 {
		t.Fatalf("consecutive = %d, want 1", n)
	}
	if held, err := m.holdAfterCeilingFailures(prd, task); held || err != nil {
		t.Fatalf("held=%v err=%v, want retry after a reset", held, err)
	}
	if task.Status == TaskReviewNeeded {
		t.Error("task moved to review despite the reset")
	}
}
