// Operator-requested: "I should also be able to edit a story so I can make
// changes without having to decompose and rely on the LLM" — add/remove a
// story or task without re-running decompose. Mirrors the existing
// EditStory/EditTaskSpec test shape (lifecycle_test.go): happy path +
// audit trail, refuses outside needs_review/revisions_asked, not-found
// errors.

package autonomous

import "testing"

func TestAddStory_AppendsAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1"}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	out, err := m.AddStory(prd.ID, "S2", "new story desc", "alice")
	if err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	if len(out.Story) != 2 {
		t.Fatalf("story count = %d, want 2", len(out.Story))
	}
	added := out.Story[1]
	if added.Title != "S2" || added.Description != "new story desc" {
		t.Fatalf("added story wrong content: %+v", added)
	}
	if added.ID == "" || added.PRDID != prd.ID {
		t.Fatalf("added story missing ID/PRDID: %+v", added)
	}
	if added.Status != StoryPending {
		t.Fatalf("added story status = %q, want pending", added.Status)
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "add_story" || last.Actor != "alice" {
		t.Fatalf("add_story decision not recorded: %+v", last)
	}
}

func TestAddStory_RequiresTitle(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	if _, err := m.AddStory(prd.ID, "", "desc", "alice"); err == nil {
		t.Fatal("expected AddStory to refuse an empty title")
	}
}

func TestAddStory_RefusesAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if _, err := m.AddStory(prd.ID, "S", "", "alice"); err == nil {
		t.Fatal("expected AddStory to refuse after approve")
	}
}

func TestRemoveStory_DeletesStoryAndItsTasks(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "S1", Tasks: []Task{{Title: "T1", Spec: "do"}}},
		{Title: "S2"},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	removedStoryID := prd.Story[0].ID
	removedTaskID := prd.Story[0].Tasks[0].ID

	out, err := m.RemoveStory(prd.ID, removedStoryID, "alice")
	if err != nil {
		t.Fatalf("RemoveStory: %v", err)
	}
	if len(out.Story) != 1 || out.Story[0].Title != "S2" {
		t.Fatalf("expected only S2 to remain, got %+v", out.Story)
	}
	if _, ok := m.Store().GetTask(removedTaskID); ok {
		t.Fatal("removed story's task should no longer be reachable via GetTask")
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "remove_story" || last.Actor != "alice" {
		t.Fatalf("remove_story decision not recorded: %+v", last)
	}
}

func TestRemoveStory_NotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	if _, err := m.RemoveStory(prd.ID, "does-not-exist", "alice"); err == nil {
		t.Fatal("expected error for nonexistent story ID")
	}
}

func TestAddTask_AppendsToStoryAndAudits(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{{Title: "T1", Spec: "existing"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	out, err := m.AddTask(prd.ID, storyID, "T2", "new task spec", "alice")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if len(out.Story[0].Tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(out.Story[0].Tasks))
	}
	added := out.Story[0].Tasks[1]
	if added.Title != "T2" || added.Spec != "new task spec" {
		t.Fatalf("added task wrong content: %+v", added)
	}
	if added.ID == "" || added.StoryID != storyID || added.PRDID != prd.ID {
		t.Fatalf("added task missing ID/StoryID/PRDID: %+v", added)
	}
	// GetTask must resolve the newly added task (reindex correctness —
	// appending to Tasks can reallocate the backing slice).
	fetched, ok := m.Store().GetTask(added.ID)
	if !ok || fetched.Title != "T2" {
		t.Fatalf("GetTask could not resolve newly added task: ok=%v fetched=%+v", ok, fetched)
	}
	// The pre-existing task must still resolve too (reindex must not lose it).
	existingID := prd.Story[0].Tasks[0].ID
	if _, ok := m.Store().GetTask(existingID); !ok {
		t.Fatal("pre-existing task became unreachable via GetTask after AddTask")
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "add_task" || last.Actor != "alice" {
		t.Fatalf("add_task decision not recorded: %+v", last)
	}
}

func TestAddTask_StoryNotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)

	if _, err := m.AddTask(prd.ID, "does-not-exist", "T", "spec", "alice"); err == nil {
		t.Fatal("expected error for nonexistent story ID")
	}
}

func TestRemoveTask_DeletesOneTaskKeepsOthers(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{
		{Title: "T1", Spec: "keep"},
		{Title: "T2", Spec: "remove me"},
	}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID
	keepID := prd.Story[0].Tasks[0].ID
	removeID := prd.Story[0].Tasks[1].ID

	out, err := m.RemoveTask(prd.ID, storyID, removeID, "alice")
	if err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	if len(out.Story[0].Tasks) != 1 || out.Story[0].Tasks[0].ID != keepID {
		t.Fatalf("expected only the kept task to remain, got %+v", out.Story[0].Tasks)
	}
	if _, ok := m.Store().GetTask(removeID); ok {
		t.Fatal("removed task should no longer be reachable via GetTask")
	}
	if _, ok := m.Store().GetTask(keepID); !ok {
		t.Fatal("kept task became unreachable via GetTask after RemoveTask")
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "remove_task" || last.Actor != "alice" {
		t.Fatalf("remove_task decision not recorded: %+v", last)
	}
}

func TestRemoveTask_NotFound(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{{Title: "T1", Spec: "x"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDNeedsReview
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID

	if _, err := m.RemoveTask(prd.ID, storyID, "does-not-exist", "alice"); err == nil {
		t.Fatal("expected error for nonexistent task ID")
	}
}

// TestStructuralEdits_WorkOnCancelledPRD covers the "operator cancelled a
// running PRD to fix a task's spec, then wants to edit and restart it"
// flow: a cancelled PRD must accept EditTaskSpec and AddTask, not just
// needs_review/revisions_asked, without any reset_to_draft.
func TestStructuralEdits_WorkOnCancelledPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{{Title: "T1", Spec: "old spec"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDCancelled
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID
	taskID := prd.Story[0].Tasks[0].ID

	out, err := m.EditTaskSpec(prd.ID, taskID, "new spec with reference URL", "alice")
	if err != nil {
		t.Fatalf("EditTaskSpec should work on a cancelled PRD: %v", err)
	}
	if out.Story[0].Tasks[0].Spec != "new spec with reference URL" {
		t.Fatalf("task spec not updated: %+v", out.Story[0].Tasks[0])
	}

	out, err = m.AddTask(prd.ID, storyID, "T2", "spec", "alice")
	if err != nil {
		t.Fatalf("AddTask should work on a cancelled PRD: %v", err)
	}
	if len(out.Story[0].Tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(out.Story[0].Tasks))
	}
}

func TestStructuralEdits_RefuseAfterApprove(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{{Title: "T1", Spec: "x"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	storyID := prd.Story[0].ID
	taskID := prd.Story[0].Tasks[0].ID

	if _, err := m.RemoveStory(prd.ID, storyID, "alice"); err == nil {
		t.Error("expected RemoveStory to refuse after approve")
	}
	if _, err := m.AddTask(prd.ID, storyID, "T2", "spec", "alice"); err == nil {
		t.Error("expected AddTask to refuse after approve")
	}
	if _, err := m.RemoveTask(prd.ID, storyID, taskID, "alice"); err == nil {
		t.Error("expected RemoveTask to refuse after approve")
	}
}
