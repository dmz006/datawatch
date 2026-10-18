package autonomous

import (
	"strings"
	"testing"
)

// overrideFixture builds a PRD with one story and one task, then pins its
// lifecycle status. cfg, when non-nil, replaces the manager config first.
func overrideFixture(t *testing.T, status PRDStatus, cfg *Config) (*Manager, *PRD) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	if cfg != nil {
		m.SetConfig(*cfg)
	}
	prd, err := m.CreatePRD("spec", "/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{
		Title: "Story",
		Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = status
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func overrideStr(s string) *string { return &s }

func TestTaskOverride_DefaultAcceptsBlocked(t *testing.T) {
	m, prd := overrideFixture(t, PRDBlocked, nil)
	storyID := prd.Story[0].ID
	taskID := prd.Story[0].Tasks[0].ID

	if _, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m", "op"); err != nil {
		t.Errorf("SetTaskLLM on blocked: %v", err)
	}
	if _, err := m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m", "op"); err != nil {
		t.Errorf("SetStoryLLM on blocked: %v", err)
	}
	if _, err := m.SetTaskGuardrailLLM(prd.ID, taskID, overrideStr("ollama"), nil, "op"); err != nil {
		t.Errorf("SetTaskGuardrailLLM on blocked: %v", err)
	}
	if _, err := m.SetTaskVerificationLLM(prd.ID, taskID, overrideStr("ollama"), nil, nil, "op"); err != nil {
		t.Errorf("SetTaskVerificationLLM on blocked: %v", err)
	}
	if _, err := m.SetStoryVerificationLLM(prd.ID, storyID, overrideStr("ollama"), nil, nil, "op"); err != nil {
		t.Errorf("SetStoryVerificationLLM on blocked: %v", err)
	}
}

func TestTaskOverride_DefaultRefusesRunning(t *testing.T) {
	m, prd := overrideFixture(t, PRDRunning, nil)
	taskID := prd.Story[0].Tasks[0].ID

	_, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m", "op")
	if err == nil {
		t.Fatal("SetTaskLLM on running should be refused by default")
	}
	if !strings.Contains(err.Error(), "is locked") {
		t.Errorf("error should contain \"is locked\": %v", err)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should list blocked among allowed statuses: %v", err)
	}
}

func TestTaskOverride_CustomListNarrowsToBlocked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TaskOverrideStatuses = []string{"blocked"}
	m, prd := overrideFixture(t, PRDCancelled, &cfg)
	storyID := prd.Story[0].ID

	if _, err := m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m", "op"); err == nil {
		t.Error("SetStoryLLM on cancelled should be refused when list is [blocked]")
	}

	prd.Status = PRDBlocked
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if _, err := m.SetStoryLLM(prd.ID, storyID, "ollama", "", "m", "op"); err != nil {
		t.Errorf("SetStoryLLM on blocked should succeed: %v", err)
	}
}

func TestTaskOverride_CustomListAddsRunning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TaskOverrideStatuses = []string{"running", "blocked"}
	m, prd := overrideFixture(t, PRDRunning, &cfg)
	taskID := prd.Story[0].Tasks[0].ID

	if _, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m", "op"); err != nil {
		t.Errorf("SetTaskLLM on running should succeed when listed: %v", err)
	}
}

func TestTaskOverride_PRDLevelStaysLocked(t *testing.T) {
	m, prd := overrideFixture(t, PRDBlocked, nil)
	taskID := prd.Story[0].Tasks[0].ID

	// PRD-level verifier LLM keeps the pre-Run trio rule (verificationLLMLocked),
	// so it still refuses blocked. SetPRDLLM and SetPRDGuardrailLLM are not
	// covered here: they lock only on running/completed and were not changed by
	// this task.
	_, err := m.SetPRDVerificationLLM(prd.ID, overrideStr("ollama"), nil, nil, "op")
	if err == nil || !strings.Contains(err.Error(), "is locked") {
		t.Errorf("SetPRDVerificationLLM on blocked must stay locked; got %v", err)
	}

	if _, err := m.SetTaskLLM(prd.ID, taskID, "ollama", "", "m", "op"); err != nil {
		t.Errorf("SetTaskLLM on blocked should succeed while PRD-level is locked: %v", err)
	}
}

func TestValidateTaskOverrideStatuses(t *testing.T) {
	if err := ValidateTaskOverrideStatuses([]string{"blocked"}); err != nil {
		t.Errorf("[blocked] should be valid: %v", err)
	}
	if err := ValidateTaskOverrideStatuses(nil); err != nil {
		t.Errorf("nil should be valid (revert to defaults): %v", err)
	}
	err := ValidateTaskOverrideStatuses([]string{"bogus"})
	if err == nil {
		t.Fatal("[bogus] should be invalid")
	}
	if !strings.Contains(err.Error(), `unknown PRD status "bogus"`) {
		t.Errorf("error should name bogus: %v", err)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should list known statuses incl. blocked: %v", err)
	}
	err = ValidateTaskOverrideStatuses([]string{"running", "x"})
	if err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Errorf("[running x] should fail naming x; got %v", err)
	}
}

func TestEffectiveTaskOverrideStatuses(t *testing.T) {
	want := []string{"needs_review", "revisions_asked", "cancelled", "blocked"}

	got := EffectiveTaskOverrideStatuses(nil)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("nil -> %v; want %v", got, want)
	}
	got = EffectiveTaskOverrideStatuses([]string{})
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("empty -> %v; want %v", got, want)
	}
	got = EffectiveTaskOverrideStatuses([]string{"draft"})
	if len(got) != 1 || got[0] != "draft" {
		t.Errorf("[draft] -> %v; want [draft]", got)
	}
}

func TestTaskOverrideStatusesKnownFourteen(t *testing.T) {
	known := TaskOverrideStatusesKnown()
	if len(known) != 14 {
		t.Fatalf("known set has %d names; want 14: %v", len(known), known)
	}
	if err := ValidateTaskOverrideStatuses([]string{"active"}); err != nil {
		t.Errorf("legacy [active] should be valid: %v", err)
	}
	err := ValidateTaskOverrideStatuses([]string{"bogus"})
	want := `task_override_statuses: unknown PRD status "bogus" (known: draft, planning, needs_review, approved, revisions_asked, active, running, completed, rejected, cancelled, archived, blocked, failed, paused)`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v; want %q", err, want)
	}
}
