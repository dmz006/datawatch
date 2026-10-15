package autonomous

import "testing"

func bl451CompletedWithPending(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, prd := bl382RunningPRD(t)
	prd.Status = PRDCompleted
	prd.Story[0].Status = StoryCompleted
	prd.Story[0].Tasks[0].Status = TaskCompleted
	prd.Story[0].Tasks[0].SessionID = ""
	prd.Story[0].Tasks[1].Status = TaskPending
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestBL451CompletedPendingForceRecovers(t *testing.T) {
	m, prd := bl451CompletedWithPending(t)
	rev := prd.Rev
	nDec := len(prd.Decisions)
	if _, err := m.ResetTask(prd.ID, "task-1b", "operator", true); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
	if s := got.Story[0].Tasks[1].Status; s != TaskPending {
		t.Errorf("task status = %q, want pending", s)
	}
	if got.Rev <= rev {
		t.Errorf("rev = %d, want > %d", got.Rev, rev)
	}
	if len(got.Decisions) != nDec+1 {
		t.Fatalf("decisions = %d, want %d", len(got.Decisions), nDec+1)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "recover_pending_task" || d.Actor != "operator" {
		t.Errorf("decision = %+v", d)
	}
}

func TestBL451CompletedPendingNoForceErrors(t *testing.T) {
	m, prd := bl451CompletedWithPending(t)
	rev := prd.Rev
	_, err := m.ResetTask(prd.ID, "task-1b", "operator", false)
	if err == nil {
		t.Fatal("expected error without force")
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted || got.Rev != rev || len(got.Decisions) != len(prd.Decisions) {
		t.Errorf("prd changed: status=%q rev=%d", got.Status, got.Rev)
	}
}

func TestBL451FailedTaskResetUnaffected(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	prd.Status = PRDFailed
	_ = m.Store().SavePRD(prd)
	if _, err := m.ResetTask(prd.ID, "task-2b", "operator", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDRunning || got.Story[1].Tasks[1].Status != "" {
		t.Errorf("status=%q task=%q", got.Status, got.Story[1].Tasks[1].Status)
	}
	if d := got.Decisions[len(got.Decisions)-1]; d.Kind != "reset_task" {
		t.Errorf("kind = %q", d.Kind)
	}
}
