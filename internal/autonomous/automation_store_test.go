package autonomous

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// storePRDs returns one lookupPRD closure over the given map, for handing to
// NewAutomationStore in tests.
func storePRDs(t *testing.T, prds map[string]*PRD) func(string) (*PRD, bool) {
	t.Helper()
	return func(id string) (*PRD, bool) {
		prd, ok := prds[id]
		return prd, ok
	}
}

// storeMember builds one member of a test Automation for store-level tests;
// each outcome edge is a member ID, or "" for no edge.
func storeMember(id, prdID, onComplete, onBlocked, onFailed string) AutomationMember {
	return AutomationMember{
		ID:         id,
		PRDID:      prdID,
		OnComplete: onComplete,
		OnBlocked:  onBlocked,
		OnFailed:   onFailed,
	}
}

// TestAutomationStoreRoundTripRebuildsIndex covers persistence: an
// Automation written by one store must reload identically through a NEW
// store on the same directory (a daemon restart), including the rebuilt
// prdID -> automationID owner index.
func TestAutomationStoreRoundTripRebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-store-1": {ID: "prd-store-1"},
		"prd-store-2": {ID: "prd-store-2"},
	}

	s1, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	created, err := s1.Create(&Automation{
		Name: "store round-trip",
		Members: []AutomationMember{
			storeMember("m1", "prd-store-1", "m2", "", ""),
			storeMember("m2", "prd-store-2", "", "", ""),
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if a, ok := s1.Get(created.ID); !ok {
		t.Fatalf("Get(%q) not found after Create", created.ID)
	} else if a.ID != created.ID {
		t.Fatalf("Get returned ID %q, want %q", a.ID, created.ID)
	}
	found := false
	for _, a := range s1.List() {
		if a.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("List has %d Automations but none has ID %q", len(s1.List()), created.ID)
	}
	for _, prdID := range []string{"prd-store-1", "prd-store-2"} {
		if owner := s1.OwnerOf(prdID); owner != created.ID {
			t.Fatalf("OwnerOf(%q) = %q, want %q", prdID, owner, created.ID)
		}
	}

	// Simulate the daemon restart: a fresh store over the same directory
	// must reload the Automation identically and rebuild the index.
	s2, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore (restart): %v", err)
	}
	reloaded, ok := s2.Get(created.ID)
	if !ok {
		t.Fatalf("reloaded store: Get(%q) not found", created.ID)
	}
	if reloaded.Name != created.Name {
		t.Errorf("reloaded Name = %q, want %q", reloaded.Name, created.Name)
	}
	if !reflect.DeepEqual(reloaded.Members, created.Members) {
		t.Errorf("reloaded Members = %+v, want %+v", reloaded.Members, created.Members)
	}
	if reloaded.Version != created.Version {
		t.Errorf("reloaded Version = %d, want %d", reloaded.Version, created.Version)
	}
	if reloaded.EntryMemberID != created.EntryMemberID {
		t.Errorf("reloaded EntryMemberID = %q, want %q", reloaded.EntryMemberID, created.EntryMemberID)
	}
	for _, prdID := range []string{"prd-store-1", "prd-store-2"} {
		if owner := s2.OwnerOf(prdID); owner != created.ID {
			t.Errorf("reloaded OwnerOf(%q) = %q, want %q", prdID, owner, created.ID)
		}
	}
}

// TestAutomationStoreReloadSkipsTerminalOwnership covers the index rule that
// terminal Automations (completed/cancelled) own nothing: an Automation
// cancelled via Update must not claim its PRDs in a freshly loaded store.
func TestAutomationStoreReloadSkipsTerminalOwnership(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-terminal-1": {ID: "prd-terminal-1"},
	}

	s1, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	created, err := s1.Create(&Automation{
		Name:    "terminal owner",
		Members: []AutomationMember{storeMember("m1", "prd-terminal-1", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s1.Update(created.ID, 0, func(a *Automation) error {
		a.Status = AutomationCancelled
		return nil
	}); err != nil {
		t.Fatalf("Update to cancelled: %v", err)
	}
	if owner := s1.OwnerOf("prd-terminal-1"); owner != "" {
		t.Fatalf("OwnerOf after cancel = %q, want \"\"", owner)
	}

	// Reload from disk: the cancelled Automation must not claim its PRD.
	s2, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore (restart): %v", err)
	}
	if owner := s2.OwnerOf("prd-terminal-1"); owner != "" {
		t.Fatalf("reloaded OwnerOf (cancelled) = %q, want \"\"", owner)
	}
}

// TestAutomationStoreCreateRejectsOwnedTemplateAndChildPRDs covers the
// store-side enforcement of the cross-Automation membership rules: an
// already-owned PRD, a template PRD and a child PRD are all rejected.
func TestAutomationStoreCreateRejectsOwnedTemplateAndChildPRDs(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-owned":    {ID: "prd-owned"},
		"prd-template": {ID: "prd-template", IsTemplate: true},
		"prd-child":    {ID: "prd-child", ParentPRDID: "prd-parent"},
		"prd-free":     {ID: "prd-free"},
	}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	first, err := s.Create(&Automation{
		Name:    "owns the contested prd",
		Members: []AutomationMember{storeMember("m1", "prd-owned", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}

	if _, err := s.Create(&Automation{
		Name: "second claimant",
		Members: []AutomationMember{
			storeMember("m1", "prd-free", "", "", ""), // a valid member too
			storeMember("m2", "prd-owned", "", "", ""),
		},
	}); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("contested PRD: expected error containing %q, got %v", "already belongs", err)
	} else if owner := s.OwnerOf("prd-owned"); owner != first.ID {
		t.Fatalf("OwnerOf(%q) = %q, want %q", "prd-owned", owner, first.ID)
	}

	if _, err := s.Create(&Automation{
		Name:    "template claimant",
		Members: []AutomationMember{storeMember("m1", "prd-template", "", "", "")},
	}); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("template PRD: expected error containing %q, got %v", "template", err)
	}

	if _, err := s.Create(&Automation{
		Name:    "child claimant",
		Members: []AutomationMember{storeMember("m1", "prd-child", "", "", "")},
	}); err == nil || !strings.Contains(err.Error(), "child") {
		t.Fatalf("child PRD: expected error containing %q, got %v", "child", err)
	}
}

// TestAutomationStoreIndependentLocksDoNotBlock covers the per-ID mutex
// (design doc "Concurrency between Automations"): an Automation held open
// under its lock must not block a WithLock call on a DIFFERENT Automation.
func TestAutomationStoreIndependentLocksDoNotBlock(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-lock-a": {ID: "prd-lock-a"},
		"prd-lock-b": {ID: "prd-lock-b"},
	}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	a, err := s.Create(&Automation{
		Name:    "locked first",
		Members: []AutomationMember{storeMember("m1", "prd-lock-a", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	b, err := s.Create(&Automation{
		Name:    "locked second",
		Members: []AutomationMember{storeMember("m1", "prd-lock-b", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	enteredA := make(chan struct{})
	release := make(chan struct{})
	doneA := make(chan error, 1)
	go func() {
		doneA <- s.WithLock(a.ID, func() error {
			close(enteredA)
			<-release
			return nil
		})
	}()
	<-enteredA

	// A's lock is still held; B's lock must not block behind it.
	lockedB := make(chan struct{})
	go func() {
		if err := s.WithLock(b.ID, func() error { return nil }); err != nil {
			t.Errorf("WithLock(B): %v", err)
		}
		close(lockedB)
	}()
	select {
	case <-lockedB:
	case <-time.After(2 * time.Second):
		t.Fatalf("WithLock(%q) did not complete within 2s while a different Automation's lock was held", b.ID)
	}

	close(release)
	if err := <-doneA; err != nil {
		t.Fatalf("WithLock(A) after release: %v", err)
	}
}

// TestAutomationStoreSameIDSerializes covers the same-Automation rule: two
// concurrent Updates on one Automation must not race and the Version
// accounting stays exact (1 per successful update).
func TestAutomationStoreSameIDSerializes(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-serialize": {ID: "prd-serialize"},
	}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	a, err := s.Create(&Automation{
		Name:    "same id serializes",
		Members: []AutomationMember{storeMember("m1", "prd-serialize", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	parseCount := func(description string) int {
		n, err := strconv.Atoi(strings.TrimPrefix(description, "count: "))
		if err != nil {
			return 0
		}
		return n
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update(a.ID, 0, func(x *Automation) error {
				x.Description = "count: " + strconv.Itoa(parseCount(x.Description)+1)
				return nil
			}); err != nil {
				t.Errorf("Update: %v", err)
				return
			}
			successes.Add(1)
		}()
	}
	wg.Wait()

	final, ok := s.Get(a.ID)
	if !ok {
		t.Fatalf("Get(%q) not found after concurrent Updates", a.ID)
	}
	want := 1 + int(successes.Load())
	if int(final.Version) != want {
		t.Fatalf("Version = %d, want %d (1 per successful update)", final.Version, want)
	}
	if got := parseCount(final.Description); got != int(successes.Load()) {
		t.Errorf("counter = %d, want %d successful updates reflected in Description", got, successes.Load())
	}
}

// TestAutomationStoreExpectedVersionConflict covers optimistic concurrency:
// an Update whose expectedVersion is stale fails with a 409 naming the
// current version and leaves the Automation unchanged.
func TestAutomationStoreExpectedVersionConflict(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-conflict": {ID: "prd-conflict"},
	}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	a, err := s.Create(&Automation{
		Name:    "expected version",
		Members: []AutomationMember{storeMember("m1", "prd-conflict", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := s.Update(a.ID, 1, func(x *Automation) error {
		x.Description = "first update"
		return nil
	})
	if err != nil {
		t.Fatalf("Update with current version: %v", err)
	}
	if first.Version != 2 {
		t.Fatalf("Version after first Update = %d, want 2", first.Version)
	}

	if _, err := s.Update(a.ID, 1, func(x *Automation) error {
		x.Description = "stale writer wins?"
		return nil
	}); err == nil || !strings.HasPrefix(err.Error(), "409: ") || !strings.Contains(err.Error(), "current version") {
		t.Fatalf("stale expectedVersion: expected error starting with %q and containing %q, got %v", "409: ", "current version", err)
	}

	final, ok := s.Get(a.ID)
	if !ok {
		t.Fatalf("Get(%q) not found after rejected Update", a.ID)
	}
	if final.Version != 2 || final.Description != "first update" {
		t.Errorf("rejected Update mutated state: Version = %d, Description = %q", final.Version, final.Description)
	}
}

// TestAutomationStoreDeleteStatusRules covers the delete rules: a draft
// deletes and frees the index, an active Automation refuses with a 409 and
// a missing ID is a 404.
func TestAutomationStoreDeleteStatusRules(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{
		"prd-draft":  {ID: "prd-draft"},
		"prd-active": {ID: "prd-active"},
	}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	draft, err := s.Create(&Automation{
		Name:    "draft for deletion",
		Members: []AutomationMember{storeMember("m1", "prd-draft", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create draft: %v", err)
	}
	active, err := s.Create(&Automation{
		Name:    "active for deletion",
		Members: []AutomationMember{storeMember("m1", "prd-active", "", "", "")},
	})
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	if _, err := s.Update(active.ID, 0, func(a *Automation) error {
		a.Status = AutomationActive
		return nil
	}); err != nil {
		t.Fatalf("Update to active: %v", err)
	}

	if err := s.Delete(draft.ID); err != nil {
		t.Fatalf("Delete draft: %v", err)
	}
	if _, ok := s.Get(draft.ID); ok {
		t.Fatalf("draft still present after Delete")
	}
	if owner := s.OwnerOf("prd-draft"); owner != "" {
		t.Fatalf("OwnerOf(%q) after Delete = %q, want \"\"", "prd-draft", owner)
	}

	if err := s.Delete(active.ID); err == nil || !strings.HasPrefix(err.Error(), "409: ") {
		t.Fatalf("Delete active: expected error starting with %q, got %v", "409: ", err)
	}
	if _, ok := s.Get(active.ID); !ok {
		t.Fatalf("active Automation was deleted despite the refusal")
	}

	if err := s.Delete("no-such-id"); err == nil || !strings.HasPrefix(err.Error(), "404: ") {
		t.Fatalf("Delete missing: expected error starting with %q, got %v", "404: ", err)
	}
}
