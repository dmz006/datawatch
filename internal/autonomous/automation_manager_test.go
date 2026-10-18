package autonomous

import (
	"strings"
	"testing"
)

// makeAutomationMemberPRDs creates n member PRDs for the manager under dir.
func makeAutomationMemberPRDs(t *testing.T, m *Manager, n int) []*PRD {
	t.Helper()
	prds := make([]*PRD, 0, n)
	for i := 0; i < n; i++ {
		prd, err := m.store.CreatePRD("spec member", "/p", "", "", "")
		if err != nil {
			t.Fatalf("CreatePRD %d: %v", i, err)
		}
		prds = append(prds, prd)
	}
	return prds
}

func TestAutomationManager_CRUD(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p1, p2 := makeAutomationMemberPRDs(t, m, 2)[0], makeAutomationMemberPRDs(t, m, 2)[1]

	a, err := m.CreateAutomation(AutomationInput{
		Name:    "cve sweep",
		Members: []AutomationMember{{PRDID: p1.ID}, {PRDID: p2.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != AutomationDraft {
		t.Errorf("new automation status = %q, want draft", a.Status)
	}
	if a.Version != 1 {
		t.Errorf("new automation version = %d, want 1", a.Version)
	}
	if len(a.ID) != 8 {
		t.Errorf("automation ID %q: want 8 chars, got %d", a.ID, len(a.ID))
	}
	if len(a.Members) != 2 || a.EntryMemberID != a.Members[0].ID {
		t.Errorf("entry member should default to the first member: entry %q, members %+v", a.EntryMemberID, a.Members)
	}

	got, ok := m.GetAutomation(a.ID)
	if !ok || got.Name != "cve sweep" {
		t.Fatalf("GetAutomation: got %+v, ok=%v", got, ok)
	}
	if n := len(m.ListAutomations()); n != 1 {
		t.Fatalf("want 1 automation listed, got %d", n)
	}

	updated, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          a.Name + "-2",
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: p1.ID}, {PRDID: p2.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "cve sweep-2" || updated.Version != 2 {
		t.Fatalf("UpdateAutomation: name=%q version=%d, want %q and 2", updated.Name, updated.Version, "cve sweep-2")
	}

	if err := m.DeleteAutomation(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.GetAutomation(a.ID); ok {
		t.Fatal("automation should be gone after delete")
	}
}

func TestAutomationManager_MembershipSyncsPRDOwnership(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p1 := makeAutomationMemberPRDs(t, m, 2)[0]
	p2 := makeAutomationMemberPRDs(t, m, 2)[1]
	check := func(t *testing.T, prdID, want string) {
		t.Helper()
		prd, ok := m.store.GetPRD(prdID)
		if !ok {
			t.Fatalf("prd %q not found", prdID)
		}
		if prd.AutomationID != want {
			t.Fatalf("prd %s AutomationID = %q, want %q", prdID, prd.AutomationID, want)
		}
	}

	a, err := m.CreateAutomation(AutomationInput{
		Name:    "ownership",
		Members: []AutomationMember{{PRDID: p1.ID}, {PRDID: p2.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	check(t, p1.ID, a.ID)
	check(t, p2.ID, a.ID)

	// Drop p2 from membership: only p2's pointer is cleared.
	if _, err := m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name:          a.Name,
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: p1.ID}},
	}, "operator"); err != nil {
		t.Fatal(err)
	}
	check(t, p1.ID, a.ID)
	check(t, p2.ID, "")

	// Deleting the Automation clears the remaining member too.
	if err := m.DeleteAutomation(a.ID); err != nil {
		t.Fatal(err)
	}
	check(t, p1.ID, "")
	check(t, p2.ID, "")
}

func TestAutomationManager_RejectsContestedTemplateAndChildPRDs(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	members := makeAutomationMemberPRDs(t, m, 4)
	owned := members[0]
	templated := members[1]
	child := members[2]

	ownedBy, err := m.CreateAutomation(AutomationInput{
		Name:    "owner",
		Members: []AutomationMember{{PRDID: owned.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateAutomation(AutomationInput{
		Name:    "contest",
		Members: []AutomationMember{{PRDID: owned.ID}},
	}, "operator"); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("contested PRD: want error containing %q, got %v", "already belongs", err)
	}
	if err := m.DeleteAutomation(ownedBy.ID); err != nil {
		t.Fatal(err)
	}

	templated.IsTemplate = true
	if err := m.store.SavePRD(templated); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateAutomation(AutomationInput{
		Name:    "template member",
		Members: []AutomationMember{{PRDID: templated.ID}},
	}, "operator"); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("template PRD: want error containing %q, got %v", "template", err)
	}

	child.ParentPRDID = owned.ID
	if err := m.store.SavePRD(child); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateAutomation(AutomationInput{
		Name:    "child member",
		Members: []AutomationMember{{PRDID: child.ID}},
	}, "operator"); err == nil || !strings.Contains(err.Error(), "child") {
		t.Fatalf("child PRD: want error containing %q, got %v", "child", err)
	}
}

func TestAutomationManager_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p1 := makeAutomationMemberPRDs(t, m, 2)[0]
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "persistent",
		Members: []AutomationMember{{PRDID: p1.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}

	// A second Manager on the same directory is the restart: it must see
	// the stored Automation and the PRD ownership it stamped.
	m2, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m2.GetAutomation(a.ID)
	if !ok || got.Name != "persistent" || got.Version != a.Version {
		t.Fatalf("GetAutomation after restart: got %+v, ok=%v", got, ok)
	}
	prd, ok := m2.store.GetPRD(p1.ID)
	if !ok || prd.AutomationID != a.ID {
		t.Fatalf("prd after restart: AutomationID=%q ok=%v, want %q", prd.AutomationID, ok, a.ID)
	}
	if n := len(m2.ListAutomations()); n != 1 {
		t.Fatalf("want 1 automation after restart, got %d", n)
	}
}

func TestAutomationManager_UpdateRejectsStaleVersion(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p1 := makeAutomationMemberPRDs(t, m, 2)[0]
	a, err := m.CreateAutomation(AutomationInput{
		Name:    "versioned",
		Members: []AutomationMember{{PRDID: p1.ID}},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.UpdateAutomation(a.ID, a.Version+1, AutomationInput{
		Name:          a.Name,
		EntryMemberID: a.EntryMemberID,
		Members:       []AutomationMember{{PRDID: p1.ID}},
	}, "operator")
	if err == nil || !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("stale expectedVersion: want error beginning %q, got %v", "409: ", err)
	}
}
