package autonomous

import (
	"errors"
	"strings"
	"testing"
)

// setHeldSession points the held task's last worker session at sid.
func setHeldSession(t *testing.T, m *Manager, prdID, taskID, sid string) {
	t.Helper()
	prd, _ := m.store.GetPRD(prdID)
	task, _ := findTaskInPRD(prd, taskID)
	m.store.WithLock(func() {
		task.SessionID = sid
		if a := lastTaskAttempt(task); a != nil {
			task.Attempts[len(task.Attempts)-1].SessionID = sid
		}
	})
	if err := m.store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
}

func TestFailureEscalation_AcceptCompletesTask(t *testing.T) {
	repo := snapTestRepo(t)
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, repo, nil)
	task, _ := heldTaskOf(t, m, api, prd)
	// Accept needs the task's own commit as well as the marker.
	snapTestGit(t, repo, "commit", "-q", "--allow-empty", "-m", "task("+prd.ID[:min(8, len(prd.ID))]+"): "+task.Title)
	setHeldSession(t, m, prd.ID, task.ID, "sess-done")
	m.SetSessionOutputFn(func(string) (string, bool) { return "work\nDATAWATCH_COMPLETE: all good\n", true })
	var audited string
	m.mu.Lock()
	m.auditFn = func(a string, d map[string]any) { audited = a + ":" + d["actor"].(string) }
	m.mu.Unlock()

	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionAccept}, "alice"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	got := escalationTask(t, m, prd.ID)
	if got.Status != TaskCompleted || got.ReviewSummary != nil || got.CompletedAt == nil {
		t.Fatalf("task not completed cleanly: status=%q summary=%v", got.Status, got.ReviewSummary)
	}
	if audited != "autonomous_review_accept:alice" {
		t.Fatalf("audit = %q", audited)
	}
	cur, _ := m.store.GetPRD(prd.ID)
	found := false
	for _, d := range cur.Decisions {
		if d.Kind == reviewDecisionKind && strings.Contains(d.Note, "accepted by alice") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no accept decision: %+v", cur.Decisions)
	}
}

func TestFailureEscalation_AcceptRefusedWithoutMarker(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, _ := heldTaskOf(t, m, api, prd)
	setHeldSession(t, m, prd.ID, task.ID, "sess-none")
	m.SetSessionOutputFn(func(string) (string, bool) { return "still working\n", true })

	_, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionAccept}, "alice")
	if err == nil || !strings.Contains(err.Error(), "no completion marker") {
		t.Fatalf("err = %v, want completion marker refusal", err)
	}
	if got := escalationTask(t, m, prd.ID); got.Status != TaskReviewNeeded {
		t.Fatalf("status = %q, want held", got.Status)
	}
}

func TestFailureEscalation_AcceptNotInReview(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task, _ := heldTaskOf(t, m, api, prd)
	if _, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionCancel}, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err := api.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: reviewActionAccept}, "alice")
	if !errors.Is(err, ErrTaskNotInReview) {
		t.Fatalf("err = %v, want ErrTaskNotInReview", err)
	}
}
