package autonomous

import "testing"

func escalationFixture(t *testing.T) (*Manager, *auditCapture, string, []string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cap := newAuditCapture()
	m.SetCapacityAudit(cap.recorder())
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1"}, {Title: "T2"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	return m, cap, prd.ID, []string{prd.Story[0].Tasks[0].ID, prd.Story[0].Tasks[1].ID}
}

func failuresOf(m *Manager, prdID, taskID string) int {
	prd, _ := m.Store().GetPRD(prdID)
	for _, t := range prd.Story[0].Tasks {
		if t.ID == taskID {
			return t.VerifierInfraFailures
		}
	}
	return -1
}

func TestVerifierEscalationNoneAfterOneInfraFailure(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t)
	if err := m.RecordVerifierOutcome(prdID, ids[0], true, false); err != nil {
		t.Fatal(err)
	}
	n := failuresOf(m, prdID, ids[0])
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	if b, esc := DecideVerifierBackend(n, 2, "pi-prompt", "claude-code"); esc || b != "pi-prompt" {
		t.Fatalf("got %q,%v; want configured backend, no escalation", b, esc)
	}
	if len(cap.all()) != 0 {
		t.Fatalf("unexpected audit entries: %+v", cap.all())
	}
}

func TestVerifierEscalationAfterTwoRecordsDecisionAndAudit(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t)
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	n := failuresOf(m, prdID, ids[0])
	b, esc := DecideVerifierBackend(n, 2, "pi-prompt", "claude-code")
	if !esc || b != "claude-code" {
		t.Fatalf("got %q,%v; want claude-code escalation", b, esc)
	}
	if err := m.RecordVerifierEscalation(prdID, ids[0], "pi-prompt", b, n); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	d := prd.Decisions[len(prd.Decisions)-1]
	if d.Kind != "verifier_escalation" || d.Backend != "claude-code" {
		t.Fatalf("decision = %+v", d)
	}
	for _, want := range []string{"from=pi-prompt", "to=claude-code", "consecutive_infra_failures=2"} {
		if !contains(d.Note, want) {
			t.Errorf("note %q lacks %q", d.Note, want)
		}
	}
	e := cap.only(t)
	if e.Action != VerifierEscalationAuditAction || e.Details["from_backend"] != "pi-prompt" || e.Details["to_backend"] != "claude-code" || e.Details["consecutive_infra_failures"] != 2 {
		t.Fatalf("audit entry = %+v", e)
	}
	// Escalated run finishes (even failing infra): counter resets.
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, true)
	if got := failuresOf(m, prdID, ids[0]); got != 0 {
		t.Fatalf("count after escalated run = %d, want 0", got)
	}
}

func TestVerifierEscalationNonInfraResetsCount(t *testing.T) {
	m, _, prdID, ids := escalationFixture(t)
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	_ = m.RecordVerifierOutcome(prdID, ids[0], false, false)
	if got := failuresOf(m, prdID, ids[0]); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	if b, esc := DecideVerifierBackend(failuresOf(m, prdID, ids[0]), 2, "pi-prompt", "claude-code"); esc {
		t.Fatalf("escalated to %q after reset", b)
	}
}

func TestVerifierEscalationNextTaskUsesConfiguredBackend(t *testing.T) {
	m, _, prdID, ids := escalationFixture(t)
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	_ = m.RecordVerifierOutcome(prdID, ids[0], true, false)
	if got := failuresOf(m, prdID, ids[1]); got != 0 {
		t.Fatalf("other task count = %d, want 0", got)
	}
	if b, esc := DecideVerifierBackend(failuresOf(m, prdID, ids[1]), 2, "pi-prompt", "claude-code"); esc || b != "pi-prompt" {
		t.Fatalf("next task got %q,%v", b, esc)
	}
}

func TestVerifierEscalationSameBackendDoesNothing(t *testing.T) {
	if b, esc := DecideVerifierBackend(5, 2, "claude-code", "claude-code"); esc || b != "claude-code" {
		t.Fatalf("got %q,%v", b, esc)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
