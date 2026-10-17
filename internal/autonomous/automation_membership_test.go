package autonomous

import "testing"

// TestAutomationMembershipKeepsPRDAutomationIDInSync covers
// syncAutomationMembership through the manager surface: Create stamps
// PRD.AutomationID on every member, Update moves it PRD by PRD (removed
// members cleared, new members stamped), Delete clears it on all members —
// but a clear never touches a PRD whose AutomationID points elsewhere.
func TestAutomationMembershipKeepsPRDAutomationIDInSync(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p1, _ := m.CreatePRD("one", "/p", "", "", "")
	p2, _ := m.CreatePRD("two", "/p", "", "", "")
	p3, _ := m.CreatePRD("three", "/p", "", "", "")
	want := map[string]string{p1.ID: "", p2.ID: "", p3.ID: ""}
	check := func(t *testing.T, prdID, automationID string) {
		t.Helper()
		got, ok := m.Store().GetPRD(prdID)
		if !ok {
			t.Fatalf("prd %q not found", prdID)
		}
		want[prdID] = automationID
		if got.AutomationID != automationID {
			t.Fatalf("prd %s AutomationID = %q, want %q", prdID, got.AutomationID, automationID)
		}
	}

	a, err := m.CreateAutomation(AutomationInput{
		Name:    "membership sync",
		Members: []AutomationMember{{PRDID: p1.ID}, {PRDID: p2.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	entry := a.Members[0].ID
	check(t, p1.ID, a.ID)
	check(t, p2.ID, a.ID)
	check(t, p3.ID, "")

	// Replace p2 with p3: p2 loses the pointer, p3 gains it, p1 keeps it.
	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          a.Name,
		EntryMemberID: entry,
		Members:       []AutomationMember{{PRDID: p1.ID}, {PRDID: p3.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("UpdateAutomation: %v", err)
	}
	if updated.ID != a.ID {
		t.Fatalf("UpdateAutomation returned ID %q, want %q", updated.ID, a.ID)
	}
	check(t, p1.ID, a.ID)
	check(t, p2.ID, "")
	check(t, p3.ID, a.ID)

	// p2 joins a second Automation before the first is deleted; the delete
	// must not clear p2's pointer to that other Automation.
	b, err := m.CreateAutomation(AutomationInput{
		Name:    "membership sync other",
		Members: []AutomationMember{{PRDID: p2.ID}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation (b): %v", err)
	}
	check(t, p2.ID, b.ID)

	if err := m.DeleteAutomation(a.ID); err != nil {
		t.Fatalf("DeleteAutomation: %v", err)
	}
	check(t, p1.ID, "")
	check(t, p3.ID, "")
	check(t, p2.ID, b.ID)
}
