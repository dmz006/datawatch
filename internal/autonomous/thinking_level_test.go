package autonomous

import "testing"

// TestSetPRDThinkingLevel covers Manager.SetPRDThinkingLevel: a valid
// level persists onto the PRD and records a decision, an empty level
// clears it back to the global pi.thinking_level fallback, and an
// invalid level is rejected without mutating the PRD.
func TestSetPRDThinkingLevel(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.SetPRDThinkingLevel(prd.ID, "medium", "operator")
	if err != nil {
		t.Fatalf("SetPRDThinkingLevel(medium): %v", err)
	}
	if updated.ThinkingLevel != "medium" {
		t.Fatalf("ThinkingLevel = %q, want %q", updated.ThinkingLevel, "medium")
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "set_thinking_level" {
			found = true
		}
	}
	if !found {
		t.Fatal("no set_thinking_level decision recorded")
	}

	// Empty clears it.
	updated, err = m.SetPRDThinkingLevel(prd.ID, "", "operator")
	if err != nil {
		t.Fatalf("SetPRDThinkingLevel(\"\"): %v", err)
	}
	if updated.ThinkingLevel != "" {
		t.Fatalf("ThinkingLevel = %q after clear, want empty", updated.ThinkingLevel)
	}

	// Invalid level rejected, PRD untouched.
	if _, err := m.SetPRDThinkingLevel(prd.ID, "extra", "operator"); err == nil {
		t.Fatal("expected error for invalid thinking_level, got nil")
	}
	after, _ := m.store.GetPRD(prd.ID)
	if after.ThinkingLevel != "" {
		t.Fatalf("PRD mutated by a rejected invalid level: ThinkingLevel = %q", after.ThinkingLevel)
	}

	// Unknown PRD id.
	if _, err := m.SetPRDThinkingLevel("does-not-exist", "high", "operator"); err == nil {
		t.Fatal("expected error for unknown prd id, got nil")
	}
}

// TestSetStoryThinkingLevel covers the story-level setter: the override
// lands on the named story only, an invalid level is rejected, and an
// unknown story id errors without mutation.
func TestSetStoryThinkingLevel(t *testing.T) {
	m, prd := overrideFixture(t, PRDBlocked, nil)
	if len(prd.Story) != 1 {
		t.Fatalf("want 1 story, got %d", len(prd.Story))
	}
	storyID := prd.Story[0].ID

	updated, err := m.SetStoryThinkingLevel(prd.ID, storyID, "low", "operator")
	if err != nil {
		t.Fatalf("SetStoryThinkingLevel(low): %v", err)
	}
	if updated.Story[0].ThinkingLevel != "low" {
		t.Fatalf("story ThinkingLevel = %q, want %q", updated.Story[0].ThinkingLevel, "low")
	}

	// Clear.
	updated, err = m.SetStoryThinkingLevel(prd.ID, storyID, "", "operator")
	if err != nil {
		t.Fatalf("SetStoryThinkingLevel(\"\"): %v", err)
	}
	if updated.Story[0].ThinkingLevel != "" {
		t.Fatalf("story ThinkingLevel = %q after clear, want empty", updated.Story[0].ThinkingLevel)
	}

	// Invalid level rejected.
	if _, err := m.SetStoryThinkingLevel(prd.ID, storyID, "ultra", "operator"); err == nil {
		t.Fatal("expected error for invalid thinking_level, got nil")
	}
	// Unknown story id.
	if _, err := m.SetStoryThinkingLevel(prd.ID, "no-such-story", "high", "operator"); err == nil {
		t.Fatal("expected error for unknown story id, got nil")
	}
}

// TestSetTaskThinkingLevel covers the task-level setter (most specific
// in the chain), including rejection of invalid levels and unknown task ids.
func TestSetTaskThinkingLevel(t *testing.T) {
	m, prd := overrideFixture(t, PRDBlocked, nil)
	if len(prd.Story[0].Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(prd.Story[0].Tasks))
	}
	taskID := prd.Story[0].Tasks[0].ID

	updated, err := m.SetTaskThinkingLevel(prd.ID, taskID, "high", "operator")
	if err != nil {
		t.Fatalf("SetTaskThinkingLevel(high): %v", err)
	}
	if updated.Story[0].Tasks[0].ThinkingLevel != "high" {
		t.Fatalf("task ThinkingLevel = %q, want %q", updated.Story[0].Tasks[0].ThinkingLevel, "high")
	}

	// Clear.
	updated, err = m.SetTaskThinkingLevel(prd.ID, taskID, "", "operator")
	if err != nil {
		t.Fatalf("SetTaskThinkingLevel(\"\"): %v", err)
	}
	if updated.Story[0].Tasks[0].ThinkingLevel != "" {
		t.Fatalf("task ThinkingLevel = %q after clear, want empty", updated.Story[0].Tasks[0].ThinkingLevel)
	}

	// Invalid level rejected, PRD untouched.
	if _, err := m.SetTaskThinkingLevel(prd.ID, taskID, "max", "operator"); err == nil {
		t.Fatal("expected error for invalid thinking_level, got nil")
	}
	after, _ := m.store.GetPRD(prd.ID)
	if after.Story[0].Tasks[0].ThinkingLevel != "" {
		t.Fatalf("task mutated by a rejected invalid level: %q", after.Story[0].Tasks[0].ThinkingLevel)
	}
	// Unknown task id.
	if _, err := m.SetTaskThinkingLevel(prd.ID, "no-such-task", "high", "operator"); err == nil {
		t.Fatal("expected error for unknown task id, got nil")
	}
}

// TestResolveThinkingLevelPriority pins the resolution chain:
// task → story → PRD → global config. Each level must win when set,
// and empty levels must fall through without shadowing the next one.
func TestResolveThinkingLevelPriority(t *testing.T) {
	cases := []struct {
		name   string
		task   *Task
		story  *Story
		prd    *PRD
		global string
		want   string
	}{
		{"task overrides story, prd, global",
			&Task{ThinkingLevel: "low"}, &Story{ThinkingLevel: "medium"}, &PRD{ThinkingLevel: "high"}, "off", "low"},
		{"story overrides prd, global",
			&Task{}, &Story{ThinkingLevel: "medium"}, &PRD{ThinkingLevel: "high"}, "off", "medium"},
		{"prd overrides global",
			&Task{}, &Story{}, &PRD{ThinkingLevel: "high"}, "off", "high"},
		{"all empty falls through to global",
			&Task{}, &Story{}, &PRD{}, "minimal", "minimal"},
		{"all empty and no global is empty (backend default applies)",
			&Task{}, &Story{}, &PRD{}, "", ""},
		{"nil story and task fall through to prd",
			nil, nil, &PRD{ThinkingLevel: "high"}, "off", "high"},
		{"nil everything uses global",
			nil, nil, &PRD{}, "medium", "medium"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveThinkingLevel(tc.task, tc.story, tc.prd, tc.global); got != tc.want {
				t.Fatalf("resolveThinkingLevel = %q, want %q", got, tc.want)
			}
		})
	}
}
