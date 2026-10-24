package autonomous

import (
	"slices"
	"strings"
	"testing"
)

// notifyTestManager builds a Manager with the package's decompose stub —
// enough to create PRD shells without a live LLM.
func notifyTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// TestSetPRDNotifyEvents — set, then replace, then clear the per-Automaton
// notify_events list. An empty or nil list clears the field to inherit
// from the next tier.
func TestSetPRDNotifyEvents(t *testing.T) {
	m := notifyTestManager(t)
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if len(prd.NotifyEvents) != 0 {
		t.Fatalf("fresh Automaton has NotifyEvents = %v, want empty (inherit)", prd.NotifyEvents)
	}

	// Set an explicit list.
	set, err := m.SetPRDNotifyEvents(prd.ID, []string{"prd.blocked", "prd.completed"})
	if err != nil {
		t.Fatalf("SetPRDNotifyEvents(set): %v", err)
	}
	if !slices.Equal(set.NotifyEvents, []string{"prd.blocked", "prd.completed"}) {
		t.Fatalf("after set: NotifyEvents = %v, want [prd.blocked prd.completed]", set.NotifyEvents)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok || !slices.Equal(got.NotifyEvents, []string{"prd.blocked", "prd.completed"}) {
		t.Fatalf("stored after set: %v ok=%v", got, ok)
	}

	// Replace with a different list.
	replace, err := m.SetPRDNotifyEvents(prd.ID, []string{"task.completed"})
	if err != nil {
		t.Fatalf("SetPRDNotifyEvents(replace): %v", err)
	}
	if !slices.Equal(replace.NotifyEvents, []string{"task.completed"}) {
		t.Fatalf("after replace: NotifyEvents = %v, want [task.completed]", replace.NotifyEvents)
	}

	// Clear with an empty list — back to inherit.
	cleared, err := m.SetPRDNotifyEvents(prd.ID, []string{})
	if err != nil {
		t.Fatalf("SetPRDNotifyEvents(clear): %v", err)
	}
	if len(cleared.NotifyEvents) != 0 {
		t.Fatalf("after clear: NotifyEvents = %v, want empty", cleared.NotifyEvents)
	}

	// Missing Automaton errors.
	if _, err := m.SetPRDNotifyEvents("no-such-prd", []string{"prd.blocked"}); err == nil {
		t.Fatalf("SetPRDNotifyEvents for missing Automaton: want error")
	}
}

// TestSetPRDNotifyEvents_errorSurfacesValidList — a bad entry is rejected
// with the valid kinds in the message, and the stored value is unchanged;
// a valid entry succeeds.
func TestSetPRDNotifyEvents_errorSurfacesValidList(t *testing.T) {
	m := notifyTestManager(t)
	proj := t.TempDir()
	prd, err := m.CreatePRD("spec", proj, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.SetPRDNotifyEvents(prd.ID, []string{"prd.blocked"}); err != nil {
		t.Fatalf("SetPRDNotifyEvents(valid): %v", err)
	}

	_, err = m.SetPRDNotifyEvents(prd.ID, []string{"no.such.kind"})
	if err == nil {
		t.Fatalf("SetPRDNotifyEvents(invalid): want error")
	}
	if !strings.Contains(err.Error(), "prd.blocked") {
		t.Errorf("invalid-entry error = %q, want it to mention a valid kind (prd.blocked)", err)
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok || !slices.Equal(got.NotifyEvents, []string{"prd.blocked"}) {
		t.Errorf("stored list after invalid write: %v (ok=%v), want [prd.blocked]", got, ok)
	}

	// A valid entry still succeeds.
	if _, err := m.SetPRDNotifyEvents(prd.ID, []string{"task.blocked", "task.session_started"}); err != nil {
		t.Errorf("SetPRDNotifyEvents(valid multi): %v", err)
	}
}

// TestGuardrailProfileNotifyEvents_create — creating a profile validates
// and stores the list; an invalid list creates nothing.
func TestGuardrailProfileNotifyEvents_create(t *testing.T) {
	m := notifyTestManager(t)

	p, err := m.CreateGuardrailProfile("ne-create", "events", []string{"sast-scan"}, nil,
		[]string{"prd.blocked", "prd.completed"})
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}
	if !slices.Equal(p.NotifyEvents, []string{"prd.blocked", "prd.completed"}) {
		t.Fatalf("created profile NotifyEvents = %v, want [prd.blocked prd.completed]", p.NotifyEvents)
	}
	stored, ok := m.GetGuardrailProfile(p.ID)
	if !ok || !slices.Equal(stored.NotifyEvents, []string{"prd.blocked", "prd.completed"}) {
		t.Fatalf("stored profile %p (ok=%v) NotifyEvents = %v", stored, ok, stored)
	}

	// Invalid list: error and no profile created.
	before := len(m.ListGuardrailProfiles())
	_, err = m.CreateGuardrailProfile("ne-bogus", "", nil, nil, []string{"not.a.kind"})
	if err == nil {
		t.Fatalf("CreateGuardrailProfile(invalid events): want error")
	}
	if got := len(m.ListGuardrailProfiles()); got != before {
		t.Fatalf("profile count = %d after invalid create, want %d", got, before)
	}
}

// TestGuardrailProfileNotifyEvents_update_nilEmptyReplacement — on update,
// nil means "leave unchanged" and an empty (non-nil) slice clears the
// list to inherit; a non-empty slice replaces.
func TestGuardrailProfileNotifyEvents_update_nilEmptyReplacement(t *testing.T) {
	m := notifyTestManager(t)
	p, err := m.CreateGuardrailProfile("ne-update", "", nil, nil, []string{"prd.blocked"})
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}

	// nil → unchanged.
	u, err := m.UpdateGuardrailProfile(p.ID, "ne-update", "", nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateGuardrailProfile(nil events): %v", err)
	}
	if !slices.Equal(u.NotifyEvents, []string{"prd.blocked"}) {
		t.Fatalf("after nil update: NotifyEvents = %v, want [prd.blocked] (unchanged)", u.NotifyEvents)
	}

	// empty non-nil → cleared to inherit.
	u, err = m.UpdateGuardrailProfile(p.ID, "ne-update", "", nil, nil, []string{})
	if err != nil {
		t.Fatalf("UpdateGuardrailProfile(empty events): %v", err)
	}
	if len(u.NotifyEvents) != 0 {
		t.Fatalf("after empty update: NotifyEvents = %v, want empty (inherit)", u.NotifyEvents)
	}

	// non-empty non-nil → replaced.
	u, err = m.UpdateGuardrailProfile(p.ID, "ne-update", "", nil, nil,
		[]string{"task.completed", "prd.completed"})
	if err != nil {
		t.Fatalf("UpdateGuardrailProfile(set events): %v", err)
	}
	if !slices.Equal(u.NotifyEvents, []string{"task.completed", "prd.completed"}) {
		t.Fatalf("after set update: NotifyEvents = %v, want [task.completed prd.completed]", u.NotifyEvents)
	}
	stored, ok := m.GetGuardrailProfile(p.ID)
	if !ok || !slices.Equal(stored.NotifyEvents, []string{"task.completed", "prd.completed"}) {
		t.Fatalf("stored profile (ok=%v) NotifyEvents = %v, want [task.completed prd.completed]", ok, stored)
	}
}

// TestGuardrailProfileNotifyEvents_update_invalidChangesNothing — an
// invalid list on update errors, leaves the stored list alone, and (see
// the audit test) writes no audit entry.
func TestGuardrailProfileNotifyEvents_update_invalidChangesNothing(t *testing.T) {
	m := notifyTestManager(t)
	p, err := m.CreateGuardrailProfile("ne-invalid", "", nil, nil, []string{"prd.blocked"})
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}

	_, err = m.UpdateGuardrailProfile(p.ID, "ne-invalid", "", nil, nil, []string{"bogus.kind"})
	if err == nil {
		t.Fatalf("UpdateGuardrailProfile(invalid events): want error")
	}
	stored, ok := m.GetGuardrailProfile(p.ID)
	if !ok || !slices.Equal(stored.NotifyEvents, []string{"prd.blocked"}) {
		t.Fatalf("stored NotifyEvents after invalid update: %v (ok=%v), want [prd.blocked]", stored, ok)
	}
}
