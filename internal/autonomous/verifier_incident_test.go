package autonomous

import (
	"strings"
	"testing"
	"time"
)

func incidentFixture(t *testing.T) (*Manager, *auditCapture, string, []string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cap := newAuditCapture()
	m.SetCapacityAudit(cap.recorder())
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1"}, {Title: "T2"}}}}); err != nil {
		t.Fatal(err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, cap, prd.ID, []string{prd.Story[0].Tasks[0].ID, prd.Story[0].Tasks[1].ID}
}

func incidentsOf(m *Manager, prdID, taskID string) []VerifierIncidentEvent {
	prd, _ := m.Store().GetPRD(prdID)
	for _, st := range prd.Story {
		for _, tk := range st.Tasks {
			if tk.ID == taskID {
				return tk.VerifierIncidentEvents
			}
		}
	}
	return nil
}

func TestVerifierIncidentRecordsEventOnTask(t *testing.T) {
	m, _, prdID, ids := incidentFixture(t)
	idle := 10 * time.Minute
	if err := m.RecordVerifierIncident(prdID, ids[0], "pi-prompt", "model-x", idle); err != nil {
		t.Fatal(err)
	}
	evs := incidentsOf(m, prdID, ids[0])
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.TaskID != ids[0] || ev.Backend != "pi-prompt" || ev.Model != "model-x" || ev.Idle != idle {
		t.Fatalf("event = %+v; want task=%s backend=pi-prompt model=model-x idle=%v", ev, ids[0], idle)
	}
	if ev.At.IsZero() {
		t.Fatal("event has zero At")
	}
	// The other task records nothing.
	if evs := incidentsOf(m, prdID, ids[1]); len(evs) != 0 {
		t.Fatalf("other task events = %+v, want none", evs)
	}
}

func TestVerifierIncidentWritesAutomatonDecision(t *testing.T) {
	m, _, prdID, ids := incidentFixture(t)
	if err := m.RecordVerifierIncident(prdID, ids[0], "pi-prompt", "model-x", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	if len(prd.Decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(prd.Decisions))
	}
	d := prd.Decisions[len(prd.Decisions)-1]
	if d.Kind != verifierIncidentDecisionKind {
		t.Fatalf("kind = %q, want %q", d.Kind, verifierIncidentDecisionKind)
	}
	if d.Backend != "pi-prompt" || d.Model != "model-x" || d.Actor != "autonomous" {
		t.Fatalf("decision = %+v", d)
	}
	if d.FailureCategory != "infra" {
		t.Fatalf("failure category = %q, want infra", d.FailureCategory)
	}
	for _, want := range []string{"task=" + ids[0], "backend=pi-prompt", "model=model-x", "idle=5m0s", "verifier incident", "not a worker failure"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("note %q lacks %q", d.Note, want)
		}
	}
}

func TestVerifierIncidentNoteSaysAutomatonNeverPRD(t *testing.T) {
	m, _, prdID, ids := incidentFixture(t)
	if err := m.RecordVerifierIncident(prdID, ids[0], "pi-prompt", "model-x", time.Minute); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	d := prd.Decisions[len(prd.Decisions)-1]
	if !strings.Contains(d.Note, "Automaton") {
		t.Fatalf("note %q does not say Automaton", d.Note)
	}
	if strings.Contains(d.Note, "PRD") {
		t.Fatalf("note %q says PRD; operator-facing text must say Automaton", d.Note)
	}
}

func TestVerifierIncidentRecordsAuditEntry(t *testing.T) {
	m, cap, prdID, ids := incidentFixture(t)
	if err := m.RecordVerifierIncident(prdID, ids[0], "pi-prompt", "model-x", 7*time.Minute); err != nil {
		t.Fatal(err)
	}
	e := cap.only(t)
	if e.Actor != AuditActorAutonomous {
		t.Fatalf("actor = %q, want %q", e.Actor, AuditActorAutonomous)
	}
	if e.Action != VerifierIncidentAuditAction {
		t.Fatalf("action = %q, want %q", e.Action, VerifierIncidentAuditAction)
	}
	if e.Subject != ids[0] {
		t.Fatalf("subject = %q, want task %q", e.Subject, ids[0])
	}
	for k, want := range map[string]any{
		"prd_id": prdID, "task_id": ids[0], "backend": "pi-prompt", "model": "model-x", "idle": "7m0s",
	} {
		if e.Details[k] != want {
			t.Errorf("details[%q] = %v, want %v", k, e.Details[k], want)
		}
	}
}

func TestVerifierIncidentSecondIncidentAccumulates(t *testing.T) {
	m, cap, prdID, ids := incidentFixture(t)
	if err := m.RecordVerifierIncident(prdID, ids[0], "pi-prompt", "model-x", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordVerifierIncident(prdID, ids[0], "claude-code", "model-y", 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	evs := incidentsOf(m, prdID, ids[0])
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2", len(evs))
	}
	if evs[1].Backend != "claude-code" || evs[1].Idle != 2*time.Minute {
		t.Fatalf("second event = %+v", evs[1])
	}
	prd, _ := m.Store().GetPRD(prdID)
	if len(prd.Decisions) != 2 {
		t.Fatalf("decisions = %d, want 2", len(prd.Decisions))
	}
	last := prd.Decisions[len(prd.Decisions)-1]
	if last.Kind != verifierIncidentDecisionKind || last.Backend != "claude-code" || last.Model != "model-y" {
		t.Fatalf("last decision = %+v", last)
	}
	if got := cap.all(); len(got) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(got))
	}
}

func TestVerifierIncidentUnknownTaskErrors(t *testing.T) {
	m, cap, prdID, _ := incidentFixture(t)
	if err := m.RecordVerifierIncident(prdID, "nope", "pi-prompt", "model-x", time.Minute); err == nil {
		t.Fatal("expected error for unknown task")
	}
	if got := cap.all(); len(got) != 0 {
		t.Fatalf("audit entries = %+v, want none", got)
	}
	prd, _ := m.Store().GetPRD(prdID)
	if len(prd.Decisions) != 0 {
		t.Fatalf("decisions = %+v, want none", prd.Decisions)
	}
}

func TestVerifierIncidentUnknownPRDErrors(t *testing.T) {
	m, cap, _, _ := incidentFixture(t)
	if err := m.RecordVerifierIncident("nope", "task", "pi-prompt", "model-x", time.Minute); err == nil {
		t.Fatal("expected error for unknown prd")
	}
	if got := cap.all(); len(got) != 0 {
		t.Fatalf("audit entries = %+v, want none", got)
	}
}

func TestVerifierIncidentIsDistinctFromWorkerFailureKinds(t *testing.T) {
	if verifierIncidentDecisionKind == reviewNeededDecisionKind || verifierIncidentDecisionKind == infraRetryDecisionKind {
		t.Fatal("verifier incident kind must not collide with worker-failure kinds")
	}
	if VerifierIncidentAuditAction == VerifierEscalationAuditAction {
		t.Fatal("verifier incident audit action must be distinct from verifier escalation")
	}
}
