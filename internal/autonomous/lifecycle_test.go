// BL191 (v5.2.0) — review/approve/reject/edit-task/template lifecycle
// tests. Verifies the new status machine transitions, the Decisions
// audit trail, the gate on Run, and the template instantiation pass.

package autonomous

import (
	"strings"
	"testing"
)

func TestApprove_FromNeedsReview(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	out, err := m.Approve(prd.ID, "alice", "looks good")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if out.Status != PRDApproved {
		t.Fatalf("status = %s, want approved", out.Status)
	}
	if out.ApprovedBy != "alice" || out.ApprovedAt == nil {
		t.Fatalf("approved metadata not recorded: by=%q at=%v", out.ApprovedBy, out.ApprovedAt)
	}
	if len(out.Decisions) == 0 || out.Decisions[len(out.Decisions)-1].Kind != "approve" {
		t.Fatalf("approve decision not appended: %+v", out.Decisions)
	}
}

func TestApprove_RefusesIfWrongStatus(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	if _, err := m.Approve(prd.ID, "alice", ""); err == nil {
		t.Fatal("expected approve to refuse on draft status")
	}
}

func TestReject_StopsTheLine(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	out, err := m.Reject(prd.ID, "bob", "wrong direction")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if out.Status != PRDRejected {
		t.Fatalf("status = %s, want rejected", out.Status)
	}
	if out.RejectionReason != "wrong direction" {
		t.Fatalf("reason = %q", out.RejectionReason)
	}
}

func TestRequestRevision_Bumps_Counter(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	out, err := m.RequestRevision(prd.ID, "alice", "split task 2")
	if err != nil {
		t.Fatalf("RequestRevision: %v", err)
	}
	if out.Status != PRDRevisionsAsked || out.RevisionsRequested != 1 {
		t.Fatalf("got status=%s rev=%d", out.Status, out.RevisionsRequested)
	}
	out, _ = m.RequestRevision(prd.ID, "alice", "and task 3 too")
	if out.RevisionsRequested != 2 {
		t.Fatalf("revisions counter = %d, want 2", out.RevisionsRequested)
	}
}

func TestEditTaskSpec_RewritesAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1", Spec: "old"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	taskID := prd.Story[0].Tasks[0].ID

	out, err := m.EditTaskSpec(prd.ID, taskID, "new spec text", "alice")
	if err != nil {
		t.Fatalf("EditTaskSpec: %v", err)
	}
	if out.Story[0].Tasks[0].Spec != "new spec text" {
		t.Fatalf("spec not rewritten: %q", out.Story[0].Tasks[0].Spec)
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "edit_task" || last.Actor != "alice" {
		t.Fatalf("edit decision not recorded: %+v", last)
	}
}

func TestEditTaskSpec_RefusesAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1", Spec: "old"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	taskID := prd.Story[0].Tasks[0].ID

	if _, err := m.EditTaskSpec(prd.ID, taskID, "x", "alice"); err == nil {
		t.Fatal("expected EditTaskSpec to refuse after approve")
	}
}

func TestInstantiateTemplate_SubstitutesVars(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	tmpl, _ := m.CreatePRD("Add {{feature}} card to Settings", "/p", "claude", EffortNormal)
	tmpl.Title = "{{feature}} card template"
	tmpl.IsTemplate = true
	tmpl.TemplateVars = []TemplateVar{{Name: "feature", Required: true}}
	_ = m.Store().SetStories(tmpl.ID, []Story{{
		Title: "S", Tasks: []Task{{Title: "Render {{feature}}", Spec: "implement {{feature}}"}},
	}})
	_ = m.Store().SavePRD(tmpl)

	out, err := m.InstantiateTemplate(tmpl.ID, map[string]string{"feature": "RTK savings"}, "alice")
	if err != nil {
		t.Fatalf("InstantiateTemplate: %v", err)
	}
	if !strings.Contains(out.Spec, "RTK savings") || !strings.Contains(out.Title, "RTK savings") {
		t.Fatalf("vars not substituted: spec=%q title=%q", out.Spec, out.Title)
	}
	if out.Story[0].Tasks[0].Spec != "implement RTK savings" {
		t.Fatalf("task spec not substituted: %q", out.Story[0].Tasks[0].Spec)
	}
	if out.IsTemplate {
		t.Fatal("instantiated PRD should not be a template")
	}
	if out.TemplateOf != tmpl.ID {
		t.Fatalf("TemplateOf = %q, want %s", out.TemplateOf, tmpl.ID)
	}
}

func TestInstantiateTemplate_RequiredVarMissing(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	tmpl, _ := m.CreatePRD("Add {{x}} card", "/p", "", "")
	tmpl.IsTemplate = true
	tmpl.TemplateVars = []TemplateVar{{Name: "x", Required: true}}
	_ = m.Store().SavePRD(tmpl)

	if _, err := m.InstantiateTemplate(tmpl.ID, nil, "alice"); err == nil {
		t.Fatal("expected error on missing required var")
	}
}

func TestInstantiateTemplate_AppliesDefaults(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	tmpl, _ := m.CreatePRD("Greet {{who}}", "/p", "", "")
	tmpl.IsTemplate = true
	tmpl.TemplateVars = []TemplateVar{{Name: "who", Default: "world"}}
	_ = m.Store().SavePRD(tmpl)

	out, err := m.InstantiateTemplate(tmpl.ID, nil, "alice")
	if err != nil {
		t.Fatalf("InstantiateTemplate: %v", err)
	}
	if out.Spec != "Greet world" {
		t.Fatalf("default not applied: spec=%q", out.Spec)
	}
}
