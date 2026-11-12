package autonomous

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestSaveWritesDerivedObjectTags pins the section-12 rule on the save
// paths themselves (docs/plans/2026-11-06-automata-pages-redesign.md:
// "anything we can tag should be tagged", written whenever the object is
// saved): every SavePRD replaces the record's SystemTags with the full
// derived list — an Automaton in a project Automation takes its project,
// its Automation and itself; moving it to another Automation replaces the
// automation tag with the new one, old tag gone; and switching the
// project axis off in tags.placement clears the project tag on the next
// save. The stored list is always the whole derivation, never an append.
func TestSaveWritesDerivedObjectTags(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// An Automaton in a project directory, alone: its own fact is the
	// project axis plus itself — no automation yet.
	prd, err := m.CreatePRD("save tags", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	saved, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	want := func(tags ...string) []string { return tags }
	mustTags := func(label string, got, expect []string) {
		t.Helper()
		if !reflect.DeepEqual(got, expect) {
			t.Fatalf("%s: SystemTags = %v, want %v", label, got, expect)
		}
	}
	mustTags("standalone Automaton", saved.SystemTags,
		want("automaton:"+prd.ID, "project:example"))

	// Into an Automation: the save that records membership re-derives the
	// whole list and picks up the automation axis.
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "save arc one",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	saved, ok = m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	if saved.AutomationID != auto.ID {
		t.Fatalf("AutomationID = %q, want %q", saved.AutomationID, auto.ID)
	}
	mustTags("Automaton in an Automation", saved.SystemTags,
		want("automation:"+auto.ID, "automaton:"+prd.ID, "project:example"))

	// Moved to another Automation: the first is deleted (its sync releases
	// the PRD), then the second picks the PRD up as its member — whose
	// membership save re-derives the whole list, so the stored tags carry
	// the new automation axis and the old one is replaced, never an append
	// of both.
	if err := m.DeleteAutomation(auto.ID); err != nil {
		t.Fatalf("DeleteAutomation: %v", err)
	}
	auto2, err := m.CreateAutomation(AutomationInput{
		Name:    "save arc two",
		Members: []AutomationMember{{PRDID: prd.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation (second): %v", err)
	}
	saved, ok = m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	if saved.AutomationID != auto2.ID {
		t.Fatalf("AutomationID = %q, want %q", saved.AutomationID, auto2.ID)
	}
	mustTags("Automaton moved to another Automation", saved.SystemTags,
		want("automation:"+auto2.ID, "automaton:"+prd.ID, "project:example"))
	for _, tag := range saved.SystemTags {
		if tag == "automation:"+auto.ID {
			t.Fatalf("old automation tag %q survived the move: %v", tag, saved.SystemTags)
		}
	}

	// The project axis switched off in tags.placement: the next save
	// re-derives under the live config and clears the project tag; the
	// axes that still apply keep theirs.
	m.SetPlacementTagSettings(func() config.PlacementConfig {
		pc := config.DefaultPlacementConfig()
		pc.Project.Enabled = false
		return pc
	})
	p, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	saved, ok = m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	mustTags("project axis off", saved.SystemTags,
		want("automation:"+auto2.ID, "automaton:"+prd.ID))

	// A task's own save derives from its Automaton's fact (plus the stage
	// its member slot carried — none for a standalone Automaton): under the
	// same live config the task takes the same three axes it has, no more,
	// no fewer.
	task := &Task{ID: "t-save-1", PRDID: prd.ID, StoryID: "s1", Title: "task save", Spec: "a saved task"}
	if err := m.store.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	ts, ok := m.store.GetTask(task.ID)
	if !ok {
		t.Fatal("re-read task not found")
	}
	mustTags("task of the Automaton", ts.SystemTags,
		want("automation:"+auto2.ID, "automaton:"+prd.ID))
}
