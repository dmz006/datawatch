package autonomous

import "testing"

func newAttemptBackfillPRD(t *testing.T, m *Manager, attempts []TaskAttempt) *PRD {
	t.Helper()
	prd, err := m.store.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.store.WithLock(func() {
		prd.Story = []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Attempts: attempts}}}}
	})
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return prd
}

func TestAttemptCategoryBackfillCategorizesStoredAttempts(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := newAttemptBackfillPRD(t, m, []TaskAttempt{
		{Number: 1, Outcome: attemptOutcomeFailed, EndReason: attemptEndVerifierFailed, VerifierIssues: []string{"x"}},
		{Number: 2, Outcome: attemptOutcomeStalled, EndReason: attemptEndStalled},
		{Number: 3, Outcome: attemptOutcomeCancelled, EndReason: attemptEndCancelledByOperator},
		{Number: 4, Outcome: attemptOutcomeFailed, FailureCategory: "policy"},
	})
	rep := m.BackfillAttemptCategories()
	if rep.AttemptsExamined != 3 || rep.AttemptsUpdated != 3 || rep.LeftUncategorized != 0 || rep.AutomatonsSaved != 1 {
		t.Fatalf("report = %+v", rep)
	}
	got, _ := m.store.SnapshotPRD(prd.ID)
	want := []string{"code_or_test", "hung", "none", "policy"}
	for i, a := range got.Story[0].Tasks[0].Attempts {
		if a.FailureCategory != want[i] {
			t.Errorf("attempt %d category = %q, want %q", a.Number, a.FailureCategory, want[i])
		}
	}
}

func TestAttemptCategoryBackfillLeavesNoReasonBlank(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := newAttemptBackfillPRD(t, m, []TaskAttempt{{Number: 1, Outcome: attemptOutcomeFailed}})
	rep := m.BackfillAttemptCategories()
	if rep.AttemptsExamined != 1 || rep.AttemptsUpdated != 0 || rep.LeftUncategorized != 1 || rep.AutomatonsSaved != 0 {
		t.Fatalf("report = %+v", rep)
	}
	got, _ := m.store.SnapshotPRD(prd.ID)
	if c := got.Story[0].Tasks[0].Attempts[0].FailureCategory; c != "" {
		t.Errorf("category = %q, want blank", c)
	}
}

func TestAttemptCategoryBackfillIdempotent(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := newAttemptBackfillPRD(t, m, []TaskAttempt{
		{Number: 1, Outcome: attemptOutcomeFailed, EndReason: attemptEndSessionDied},
		{Number: 2, Outcome: attemptOutcomeFailed},
	})
	if rep := m.BackfillAttemptCategories(); rep.AttemptsUpdated != 1 {
		t.Fatalf("first run = %+v", rep)
	}
	before, _ := m.store.SnapshotPRD(prd.ID)
	rev := before.Rev
	rep := m.BackfillAttemptCategories()
	if rep.AttemptsUpdated != 0 || rep.AutomatonsSaved != 0 {
		t.Fatalf("second run = %+v, want no updates", rep)
	}
	after, _ := m.store.SnapshotPRD(prd.ID)
	if after.Rev != rev {
		t.Errorf("second run persisted: rev %d -> %d", rev, after.Rev)
	}
}
