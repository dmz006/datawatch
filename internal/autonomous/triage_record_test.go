// triage_record_test.go — the triage record: one record per failed/blocked
// Automaton, pure builder + store helpers. Run with:
//
//	env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestTriage
package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func triageSetup(t *testing.T, status PRDStatus) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/tmp", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Status = status
	prd.BlockedReason = "guardrail blocked it"
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func setBlockedTask(m *Manager, prdID, storyID, taskID string, task Task) *Task {
	m.Store().SetStories(prdID, []Story{{
		ID: storyID, Title: "story",
		Tasks: []Task{task},
	}})
	got, _ := m.Store().GetPRD(prdID)
	return &got.Story[0].Tasks[0]
}

func TestTriageBuilderBlocked(t *testing.T) {
	m, prd := triageSetup(t, PRDBlocked)
	tk := setBlockedTask(m, prd.ID, "s1", "t1", Task{
		ID: "t1", Title: "do the work", Status: TaskBlocked,
		Error: "blocked: capacity", UpdatedAt: time.Now(),
	})
	rec := m.BuildTriageRecordFor(prd)
	if rec == nil {
		t.Fatal("BuildTriageRecordFor returned nil for a blocked PRD")
	}
	if rec.PRDID != prd.ID {
		t.Errorf("PRDID = %q, want %q", rec.PRDID, prd.ID)
	}
	if rec.Status != string(PRDBlocked) {
		t.Errorf("Status = %q, want %q", rec.Status, PRDBlocked)
	}
	if rec.TaskID != "t1" || rec.TaskTitle != "do the work" {
		t.Errorf("task = %q/%q, want t1/do the work", rec.TaskID, rec.TaskTitle)
	}
	if rec.Error != "blocked: capacity" {
		t.Errorf("Error = %q, want %q", rec.Error, "blocked: capacity")
	}
	if rec.AutomationID != "" || rec.AutomationName != "" || rec.MemberID != "" {
		t.Errorf("standalone PRD should have empty automation fields: %+v", rec)
	}
	if rec.FirstSeenAt.IsZero() || rec.UpdatedAt.IsZero() {
		t.Errorf("timestamps unset: %+v", rec)
	}
	if rec.AlertedAt != nil || len(rec.Notes) != 0 {
		t.Errorf("fresh record must be unalerted and note-less: %+v", rec)
	}
	_ = tk
}

// TestTriageBuilderFailedPrecedence: the newest failed task wins over a
// blocked one; error text is capping-checked end to end only in length.
func TestTriageBuilderFailedPrecedence(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/tmp", "", "", "")
	prd.Status = PRDFailed
	now := time.Now()
	longErr := strings.Repeat("x", maxTriageErrorLen+500)
	m.Store().SetStories(prd.ID, []Story{{
		ID: "s1", Title: "story", Tasks: []Task{
			{ID: "tb", Title: "old blocked", Status: TaskBlocked, Error: "b", UpdatedAt: now.Add(-time.Minute)},
			{ID: "tf", Title: "failed one", Status: TaskFailed, Error: longErr, UpdatedAt: now},
		},
	}})
	got, _ := m.Store().GetPRD(prd.ID)
	rec := m.BuildTriageRecordFor(got)
	if rec == nil {
		t.Fatal("nil record for failed PRD")
	}
	if rec.Status != string(PRDFailed) {
		t.Errorf("Status = %q, want %q", rec.Status, PRDFailed)
	}
	if rec.TaskID != "tf" {
		t.Errorf("TaskID = %q, want tf (failed wins over blocked)", rec.TaskID)
	}
	if len(rec.Error) != maxTriageErrorLen {
		t.Errorf("Error len = %d, want capped %d", len(rec.Error), maxTriageErrorLen)
	}
}

func TestTriageBuilderAutomationIdentity(t *testing.T) {
	m, prd := triageSetup(t, PRDBlocked)
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "triage identity",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	rec := m.BuildTriageRecordFor(got)
	if rec == nil {
		t.Fatal("nil record")
	}
	if rec.AutomationID != a.ID || rec.AutomationName != "triage identity" {
		t.Errorf("automation = %q/%q, want %q/%q", rec.AutomationID, rec.AutomationName, a.ID, "triage identity")
	}
	if rec.MemberID == "" {
		t.Errorf("MemberID empty; want the member owning this PRD")
	}
}

func TestTriageBuilderBlockedReasonFallback(t *testing.T) {
	m, prd := triageSetup(t, PRDBlocked)
	rec := m.BuildTriageRecordFor(prd)
	if rec == nil {
		t.Fatal("nil record")
	}
	if rec.Error != "guardrail blocked it" {
		t.Errorf("Error = %q, want blocked reason fallback", rec.Error)
	}
	if rec.TaskID != "" {
		t.Errorf("TaskID = %q, want empty (no failed/blocked task)", rec.TaskID)
	}
}

func TestTriageEndReasonFromTaskField(t *testing.T) {
	m, prd := triageSetup(t, PRDBlocked)
	tk := setBlockedTask(m, prd.ID, "s1", "t1", Task{
		ID: "t1", Title: "t", Status: TaskBlocked, Error: "e",
		SessionEndReason: "structural reason", UpdatedAt: time.Now(),
	})
	prd2, _ := m.Store().GetPRD(prd.ID)
	rec := m.BuildTriageRecordFor(prd2)
	if rec == nil {
		t.Fatal("nil record")
	}
	if rec.SessionEndReason != "structural reason" {
		t.Errorf("SessionEndReason = %q, want %q", rec.SessionEndReason, "structural reason")
	}
	_ = tk
}

func TestTriageEndReasonFallsBackToDecisionNote(t *testing.T) {
	m, prd := triageSetup(t, PRDBlocked)
	// ended_sessions decision, no Task.SessionEndReason field.
	tk := setBlockedTask(m, prd.ID, "s1", "t1", Task{
		ID: "t1", Title: "t", Status: TaskBlocked, Error: "e", UpdatedAt: time.Now(),
	})
	_ = tk
	prd2, _ := m.Store().GetPRD(prd.ID)
	recordEndedSessions(prd2, &prd2.Story[0].Tasks[0], []string{"sess-1", "sess-2"}, "operator reset the task")
	rec := m.BuildTriageRecordFor(prd2)
	if rec == nil {
		t.Fatal("nil record")
	}
	if rec.SessionEndReason != "operator reset the task" {
		t.Errorf("SessionEndReason = %q, want %q", rec.SessionEndReason, "operator reset the task")
	}
	// structural field wins when both are present
	prd2, _ = m.Store().GetPRD(prd.ID)
	prd2.Story[0].Tasks[0].SessionEndReason = "field wins"
	rec = m.BuildTriageRecordFor(prd2)
	if rec.SessionEndReason != "field wins" {
		t.Errorf("SessionEndReason = %q, want field to win over decision", rec.SessionEndReason)
	}
}

func TestTriageSaveRefreshPreserves(t *testing.T) {
	m, prd := triageSetup(t, PRDFailed)
	t0 := time.Now().Truncate(time.Second)
	rec1 := BuildTriageRecord(prd, TriageRetryBudget{}, "", "", t0)
	if err := m.SaveTriageRecord(prd.ID, rec1); err != nil {
		t.Fatalf("SaveTriageRecord (fresh): %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Triage == nil {
		t.Fatal("PRD.Triage nil after save")
	}
	if !got.Triage.FirstSeenAt.Equal(t0) {
		t.Errorf("FirstSeenAt = %v, want %v", got.Triage.FirstSeenAt, t0)
	}

	// mark alerted
	alertedAt := time.Now().Add(time.Second).Truncate(time.Second)
	got.Triage.AlertedAt = &alertedAt
	if err := m.Store().SavePRD(got); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// append a note
	if err := m.AppendTriageNote(prd.ID, "alert", "escalated to operator"); err != nil {
		t.Fatalf("AppendTriageNote: %v", err)
	}

	// refresh with a fresh record that does NOT carry alert/notes — those must survive
	got, _ = m.Store().GetPRD(prd.ID)
	fresh := BuildTriageRecord(got, m.ResolvedTriageBudget(got), "", "", time.Now())
	if fresh == nil {
		t.Fatal("fresh build nil")
	}
	if err := m.SaveTriageRecord(prd.ID, fresh); err != nil {
		t.Fatalf("SaveTriageRecord (refresh): %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.Triage == nil {
		t.Fatal("PRD.Triage nil after refresh")
	}
	if !got.Triage.FirstSeenAt.Equal(t0) {
		t.Errorf("refresh lost FirstSeenAt: %v", got.Triage.FirstSeenAt)
	}
	if got.Triage.AlertedAt == nil || !got.Triage.AlertedAt.Equal(alertedAt) {
		t.Errorf("refresh lost AlertedAt: %v", got.Triage.AlertedAt)
	}
	if len(got.Triage.Notes) != 1 || got.Triage.Notes[0].Source != "alert" || got.Triage.Notes[0].Text != "escalated to operator" {
		t.Errorf("refresh lost Notes: %v", got.Triage.Notes)
	}
}

func TestTriageAppendNoteRequiresExistingRecord(t *testing.T) {
	m, prd := triageSetup(t, PRDFailed)
	if err := m.AppendTriageNote(prd.ID, "alert", "x"); err != ErrTriageNoRecord {
		t.Errorf("AppendTriageNote on fresh PRD err = %v, want ErrTriageNoRecord", err)
	}

	// with a record, appends stack
	if err := m.SaveTriageRecord(prd.ID, BuildTriageRecord(prd, TriageRetryBudget{}, "", "", time.Now())); err != nil {
		t.Fatalf("SaveTriageRecord: %v", err)
	}
	if err := m.AppendTriageNote(prd.ID, "alert", "first"); err != nil {
		t.Fatalf("AppendTriageNote 1: %v", err)
	}
	if err := m.AppendTriageNote(prd.ID, "review", "second"); err != nil {
		t.Fatalf("AppendTriageNote 2: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if len(got.Triage.Notes) != 2 || got.Triage.Notes[1].Source != "review" {
		t.Errorf("Notes = %+v, want two, last source review", got.Triage.Notes)
	}
}

func TestTriageClearMakesFieldAbsent(t *testing.T) {
	m, prd := triageSetup(t, PRDFailed)
	if err := m.SaveTriageRecord(prd.ID, BuildTriageRecord(prd, TriageRetryBudget{}, "", "", time.Now())); err != nil {
		t.Fatalf("SaveTriageRecord: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Triage == nil {
		t.Fatal("record missing before clear")
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"triage"`) {
		t.Fatalf("recorded PRD should carry triage in JSON: %s", raw)
	}
	if err := m.ClearTriageRecord(prd.ID); err != nil {
		t.Fatalf("ClearTriageRecord: %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.Triage != nil {
		t.Fatalf("Triage not nil after clear")
	}
	raw, _ = json.Marshal(got)
	if strings.Contains(string(raw), `"triage"`) {
		t.Errorf("cleared PRD still carries triage in JSON: %s", raw)
	}
}

func TestTriageListRecords(t *testing.T) {
	m, prdA := triageSetup(t, PRDFailed)
	// second PRD with its own record
	m2prd, err := m.CreatePRD("spec2", "/tmp", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	m2prd.Status = PRDBlocked
	m2prd.BlockedReason = "blocked B"
	_ = m.Store().SavePRD(m2prd)
	m2prd, _ = m.Store().GetPRD(m2prd.ID)

	if err := m.SaveTriageRecord(prdA.ID, BuildTriageRecord(prdA, TriageRetryBudget{}, "", "", time.Now())); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := m.SaveTriageRecord(m2prd.ID, BuildTriageRecord(m2prd, TriageRetryBudget{}, "", "", time.Now())); err != nil {
		t.Fatalf("save B: %v", err)
	}
	// third PRD without a record
	c, _ := m.CreatePRD("spec3", "/tmp", "", "", "")
	c.Status = PRDRunning
	_ = m.Store().SavePRD(c)

	recs := m.ListTriageRecords()
	if len(recs) != 2 {
		t.Fatalf("ListTriageRecords len = %d, want 2 (record-less PRD excluded)", len(recs))
	}
	ids := map[string]bool{}
	for _, r := range recs {
		ids[r.PRDID] = true
	}
	if !ids[prdA.ID] || !ids[m2prd.ID] {
		t.Errorf("ListTriageRecords = %v, want %s and %s", ids, prdA.ID, m2prd.ID)
	}
}

func TestTriageBuilderNilForHealthyPRD(t *testing.T) {
	m, prd := triageSetup(t, PRDRunning)
	if rec := m.BuildTriageRecordFor(prd); rec != nil {
		t.Errorf("BuildTriageRecordFor on running PRD = %+v, want nil", rec)
	}
	if rec := BuildTriageRecord(nil, TriageRetryBudget{}, "", "", time.Now()); rec != nil {
		t.Errorf("BuildTriageRecord(nil) = %+v, want nil", rec)
	}
}

func TestTriageBudgetValues(t *testing.T) {
	m, prd := triageSetup(t, PRDFailed)
	prd.RetryPolicy = map[string]int{"infra": 2, "content": 3, "scope": 1, "policy": 0}
	prd.RetryCountsByCategory = map[string]int{"infra": 1, "content": 3, "scope": 0, "policy": 1}
	prd.RetryCeilingTotal = 4
	_ = m.Store().SavePRD(prd)
	got, _ := m.Store().GetPRD(prd.ID)

	b := m.ResolvedTriageBudget(got)
	if b.Infra.Limit != 2 || b.Infra.Used != 1 || b.Infra.Left != 1 {
		t.Errorf("infra = %+v, want limit 2 used 1 left 1", b.Infra)
	}
	if b.Content.Limit != 3 || b.Content.Used != 3 || b.Content.Left != 0 {
		t.Errorf("content = %+v, want limit 3 used 3 left 0", b.Content)
	}
	if b.Scope.Limit != 1 || b.Scope.Used != 0 || b.Scope.Left != 1 {
		t.Errorf("scope = %+v, want limit 1 used 0 left 1", b.Scope)
	}
	if b.Policy.Limit != 0 || b.Policy.Used != 1 || b.Policy.Left != 0 {
		t.Errorf("policy = %+v, want limit 0 used 1 left 0 (left never negative)", b.Policy)
	}
	if b.CeilingTotal != 4 {
		t.Errorf("CeilingTotal = %d, want 4 (PRD override)", b.CeilingTotal)
	}
	if b.CeilingLeft != 0 { // spent = 1+3+0+1 = 5 > 4 → 0
		t.Errorf("CeilingLeft = %d, want 0 (never negative)", b.CeilingLeft)
	}

	// the budget is embedded on the record itself
	rec := m.BuildTriageRecordFor(got)
	if rec == nil {
		t.Fatal("nil record")
	}
	if rec.RetryBudget != b {
		t.Errorf("record budget = %+v, want %+v", rec.RetryBudget, b)
	}
}

func TestTriageSaveErrorOnUnknownPRD(t *testing.T) {
	m, _ := triageSetup(t, PRDFailed)
	if err := m.SaveTriageRecord("nope", &TriageRecord{}); err == nil {
		t.Error("SaveTriageRecord(unknown) err = nil, want error")
	}
	if err := m.ClearTriageRecord("nope"); err == nil {
		t.Error("ClearTriageRecord(unknown) err = nil, want error")
	}
}
