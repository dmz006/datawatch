package autonomous

// An Automaton can carry a tag ID that has no record, for example after a lost
// record. A later engine tag sync must keep that ID and finish: it must not
// refuse after the membership change is already saved.

import (
	"testing"
)

// danglingMember makes an Automation with one member and then removes the
// record of the member's automation tag, leaving its ID on the Automaton.
func danglingMember(t *testing.T) (*Manager, *Automation, *PRD, string) {
	t.Helper()
	m := autoTagsManager(t, true)
	prd := autoTagsPRD(t, m, "Ship a made-up toggle")
	a, err := m.CreateAutomation(AutomationInput{Name: "Alpha", Members: []AutomationMember{{PRDID: prd.ID}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	lost := mustAutoTag(t, m, "automation "+a.ID).ID
	if err := m.store.DeleteTag(lost); err != nil {
		t.Fatal(err)
	}
	return m, a, prd, lost
}

func TestMemberAddWithAMissingTagRecordSucceedsAndKeepsTheID(t *testing.T) {
	m, a, prd, lost := danglingMember(t)
	second := autoTagsPRD(t, m, "Ship a second made-up toggle")
	cur, _ := m.GetAutomation(a.ID)
	members := append(append([]AutomationMember(nil), cur.Members...), AutomationMember{PRDID: second.ID})
	if _, err := m.UpdateAutomation(a.ID, cur.Version, AutomationInput{Members: members}, "operator"); err != nil {
		t.Fatalf("adding a member failed: %v", err)
	}
	// The lost ID stays on the Automaton, so a repair can still restore it.
	autoTagsHas(t, "first member", autoTagsPRDIDs(t, m, prd.ID), lost)
	newTag := mustAutoTag(t, m, "automation "+a.ID)
	autoTagsHas(t, "first member", autoTagsPRDIDs(t, m, prd.ID), newTag.ID)
	autoTagsHas(t, "second member", autoTagsPRDIDs(t, m, second.ID), newTag.ID)
}

func TestMemberRemovalWithAMissingTagRecordSucceeds(t *testing.T) {
	m := autoTagsManager(t, true)
	op := mustTag(t, m, "operator-note", "")
	prd := autoTagsPRD(t, m, "Ship a made-up toggle")
	a, err := m.CreateAutomation(AutomationInput{Name: "Alpha", Members: []AutomationMember{{PRDID: prd.ID}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if err := m.SetPRDTags(prd.ID, append(autoTagsPRDIDs(t, m, prd.ID), op.ID)); err != nil {
		t.Fatal(err)
	}
	if err := m.store.DeleteTag(op.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteAutomation(a.ID); err != nil {
		t.Fatalf("DeleteAutomation: %v", err)
	}
	autoTagsHas(t, "former member", autoTagsPRDIDs(t, m, prd.ID), op.ID)
}

// An operator tag write still refuses an ID that has no record and is new to
// the Automaton.
func TestSetPRDTagsStillRefusesANewUnknownID(t *testing.T) {
	m := autoTagsManager(t, false)
	prd := autoTagsPRD(t, m, "Ship a made-up toggle")
	if err := m.SetPRDTags(prd.ID, []string{"nosuchtag"}); err == nil {
		t.Fatal("an unknown tag ID was accepted")
	}
}
