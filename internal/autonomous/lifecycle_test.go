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

// v5.26.32 — operator-asked story-level review/edit. Mirrors the
// EditTaskSpec test pair: rewrites + audits in needs_review, and
// refuses after approve.
func TestEditStory_RewritesAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S-old", Description: "desc-old"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, err := m.EditStory(prd.ID, storyID, "S-new", "desc-new", "alice")
	if err != nil {
		t.Fatalf("EditStory: %v", err)
	}
	if out.Story[0].Title != "S-new" || out.Story[0].Description != "desc-new" {
		t.Fatalf("story not rewritten: title=%q desc=%q", out.Story[0].Title, out.Story[0].Description)
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "edit_story" || last.Actor != "alice" {
		t.Fatalf("edit decision not recorded: %+v", last)
	}
}

func TestEditStory_TitleOnlyKeepsDescription(t *testing.T) {
	// Empty newDescription must NOT clear an existing description.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S-old", Description: "preserve me"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, _ := m.EditStory(prd.ID, storyID, "S-new", "", "alice")
	if out.Story[0].Description != "preserve me" {
		t.Fatalf("description was clobbered: %q", out.Story[0].Description)
	}
}

func TestEditStory_RefusesAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	if _, err := m.EditStory(prd.ID, storyID, "X", "", "alice"); err == nil {
		t.Fatal("expected EditStory to refuse after approve")
	}
}

// Phase 3 (v5.26.60) — per-story execution profile + approval gate.
func TestSetStoryProfile_RewritesAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, err := m.SetStoryProfile(prd.ID, storyID, "datawatch-smoke", "alice")
	if err != nil {
		t.Fatalf("SetStoryProfile: %v", err)
	}
	if out.Story[0].ExecutionProfile != "datawatch-smoke" {
		t.Fatalf("execution_profile not set: %q", out.Story[0].ExecutionProfile)
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "set_story_profile" || last.Actor != "alice" {
		t.Fatalf("decision not recorded: %+v", last)
	}
}

func TestSetStoryProfile_EmptyClears(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", ExecutionProfile: "old"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, _ := m.SetStoryProfile(prd.ID, storyID, "", "alice")
	if out.Story[0].ExecutionProfile != "" {
		t.Fatalf("expected empty cleared profile, got %q", out.Story[0].ExecutionProfile)
	}
}

func TestSetStoryProfile_RefusesAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	if _, err := m.SetStoryProfile(prd.ID, prd.Story[0].ID, "x", "alice"); err == nil {
		t.Fatal("expected refusal post-approve")
	}
}

func TestApproveStory_TransitionsAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Status: StoryAwaitingApproval}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, err := m.ApproveStory(prd.ID, storyID, "alice")
	if err != nil {
		t.Fatalf("ApproveStory: %v", err)
	}
	if !out.Story[0].Approved || out.Story[0].ApprovedBy != "alice" || out.Story[0].ApprovedAt == nil {
		t.Fatalf("approval fields not set: %+v", out.Story[0])
	}
	if out.Story[0].Status != StoryPending {
		t.Fatalf("status not transitioned awaiting_approval → pending: %s", out.Story[0].Status)
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "approve_story" || last.Actor != "alice" {
		t.Fatalf("decision not recorded: %+v", last)
	}
}

func TestApproveStory_RefusesBeforePRDApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	if _, err := m.ApproveStory(prd.ID, prd.Story[0].ID, "alice"); err == nil {
		t.Fatal("expected refusal before PRD approve")
	}
}

func TestRejectStory_BlocksAndRequiresReason(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	if _, err := m.RejectStory(prd.ID, storyID, "alice", ""); err == nil {
		t.Fatal("expected refusal on empty reason")
	}
	out, err := m.RejectStory(prd.ID, storyID, "alice", "guardrail block")
	if err != nil {
		t.Fatalf("RejectStory: %v", err)
	}
	if out.Story[0].Status != StoryBlocked {
		t.Fatalf("status not blocked: %s", out.Story[0].Status)
	}
	if out.Story[0].RejectedReason != "guardrail block" {
		t.Fatalf("reason not stored: %q", out.Story[0].RejectedReason)
	}
}

// Phase 4 (v5.26.64) — file association.
func TestSetStoryFiles_RewritesAndCaps(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	files := []string{"a.go", "b.go", "c.go"}
	out, err := m.SetStoryFiles(prd.ID, storyID, files, "alice")
	if err != nil {
		t.Fatalf("SetStoryFiles: %v", err)
	}
	if len(out.Story[0].FilesPlanned) != 3 {
		t.Fatalf("expected 3 files, got %d", len(out.Story[0].FilesPlanned))
	}
	// 50-cap
	big := make([]string, 100)
	for i := range big {
		big[i] = "f.go"
	}
	out2, _ := m.SetStoryFiles(prd.ID, storyID, big, "alice")
	if len(out2.Story[0].FilesPlanned) != 50 {
		t.Fatalf("expected 50-cap, got %d", len(out2.Story[0].FilesPlanned))
	}
}

func TestSetStoryFiles_RefusesAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	if _, err := m.SetStoryFiles(prd.ID, prd.Story[0].ID, []string{"x"}, "alice"); err == nil {
		t.Fatal("expected refusal after approve")
	}
}

func TestSetTaskFiles_RewritesAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	taskID := prd.Story[0].Tasks[0].ID

	out, err := m.SetTaskFiles(prd.ID, taskID, []string{"a.go", "b.go"}, "alice")
	if err != nil {
		t.Fatalf("SetTaskFiles: %v", err)
	}
	if len(out.Story[0].Tasks[0].FilesPlanned) != 2 {
		t.Fatalf("expected 2 files, got %d", len(out.Story[0].Tasks[0].FilesPlanned))
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "set_task_files" || last.Actor != "alice" {
		t.Fatalf("decision not recorded: %+v", last)
	}
}

func TestRecordTaskFilesTouched_PostSpawnNoLock(t *testing.T) {
	// Daemon-internal hook fires after worker session ends — no
	// lock-after-approve gate.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	taskID := prd.Story[0].Tasks[0].ID

	if err := m.RecordTaskFilesTouched(prd.ID, taskID, []string{"actual.go"}); err != nil {
		t.Fatalf("RecordTaskFilesTouched: %v", err)
	}
	out, _ := m.Store().GetPRD(prd.ID)
	if len(out.Story[0].Tasks[0].FilesTouched) != 1 || out.Story[0].Tasks[0].FilesTouched[0] != "actual.go" {
		t.Fatalf("FilesTouched not recorded: %+v", out.Story[0].Tasks[0].FilesTouched)
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
