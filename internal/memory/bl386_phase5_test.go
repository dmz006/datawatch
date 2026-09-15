// BL386 Phase 5 — scope inventory + scoped sweep tests.
//
// TC-1: ScopeInventory returns distinct (role,session) groups with counts
// TC-2: MemoryStats includes scope breakdown via Inventory
// TC-3: ScopedSweep(session-local) only deletes entries with session-local role
// TC-4: ScopedSweep(no scope) falls back to global Prune

package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBL386_ScopeInventory_ReturnsAllScopes(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/inv-proj", PRDID: "prd-inv1"}
	storyRef := ScopeRef{Scope: ScopeStoryShared, Project: "/inv-proj", StoryID: "story-inv1"}
	projRef := ScopeRef{Scope: ScopeProjectShared, Project: "/inv-proj"}

	pdir, prole, _ := prdRef.Resolve()
	_, _ = b.Save(pdir, "prd inv A", "", prole, "", nil)
	_, _ = b.Save(pdir, "prd inv B", "", prole, "", nil)

	sdir, srole, _ := storyRef.Resolve()
	_, _ = b.Save(sdir, "story inv A", "", srole, "", nil)

	xdir, xrole, _ := projRef.Resolve()
	_, _ = b.Save(xdir, "proj inv A", "", xrole, "", nil)
	_, _ = b.Save(xdir, "proj inv B", "", xrole, "", nil)
	_, _ = b.Save(xdir, "proj inv C", "", xrole, "", nil)

	inv, ok := b.(InventoryBackend)
	if !ok {
		t.Skip("backend does not implement InventoryBackend")
	}

	entries, err := inv.Inventory("/inv-proj")
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}

	// Expect 3 distinct rows: "prd/prd-inv1" (2), "story/story-inv1" (1), "" (3)
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Role] = e.Count
	}
	if counts["prd/prd-inv1"] != 2 {
		t.Errorf("expected 2 for prd/prd-inv1, got %d", counts["prd/prd-inv1"])
	}
	if counts["story/story-inv1"] != 1 {
		t.Errorf("expected 1 for story/story-inv1, got %d", counts["story/story-inv1"])
	}
	if counts[""] != 3 {
		t.Errorf("expected 3 for project-shared (role=''), got %d", counts[""])
	}
}

func TestBL386_MemoryStats_IncludesScopeBreakdown(t *testing.T) {
	b := newScopeTestStore(t)

	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/stats-proj", PRDID: "prd-stats"}
	dir, role, _ := prdRef.Resolve()
	_, _ = b.Save(dir, "stats prd memory A", "", role, "", nil)
	_, _ = b.Save(dir, "stats prd memory B", "", role, "", nil)

	inv, ok := b.(InventoryBackend)
	if !ok {
		t.Skip("backend does not implement InventoryBackend")
	}

	entries, err := inv.Inventory("/stats-proj")
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}

	// The inventory should report 2 entries for prd-shared scope
	total := 0
	for _, e := range entries {
		total += e.Count
	}
	if total != 2 {
		t.Errorf("expected total 2 from stats-proj inventory, got %d", total)
	}
}

func TestBL386_ScopedSweep_SessionLocal_OnlySweesScopeSessionLocal(t *testing.T) {
	dir := t.TempDir()
	b, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer b.Close() //nolint:errcheck

	sessRef := ScopeRef{Scope: ScopeSessionLocal, Project: "/sweep-proj", SessionID: "sweep-sess"}
	prdRef := ScopeRef{Scope: ScopePRDShared, Project: "/sweep-proj", PRDID: "prd-sweep"}

	sessDir, sessRole, _ := sessRef.Resolve()
	prdDir, prdRole, _ := prdRef.Resolve()

	_, _ = b.Save(sessDir, "session mem sweep A", "", sessRole, "sweep-sess", nil)
	_, _ = b.Save(sessDir, "session mem sweep B", "", sessRole, "sweep-sess", nil)
	_, _ = b.Save(prdDir, "prd mem sweep keep", "", prdRole, "", nil)

	// Sweep session-local: negative duration = cutoff in the future = prunes all
	n, err := SweepScopedByAge(b, sessRef, -time.Second, false)
	if err != nil {
		t.Fatalf("SweepScopedByAge: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 session-local rows pruned, got %d", n)
	}

	// prd-shared should be untouched
	prdRows, _ := b.ListByRole(prdDir, prdRole, 100)
	if len(prdRows) != 1 {
		t.Errorf("expected 1 prd-shared row remaining, got %d", len(prdRows))
	}
}

func TestBL386_ScopedSweep_GlobalFallback_WhenNoScopeProvided(t *testing.T) {
	dir := t.TempDir()
	b, err := NewStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer b.Close() //nolint:errcheck

	proj := "/global-sweep"
	for _, content := range []string{"global A", "global B", "global C"} {
		_, _ = b.Save(proj, content, "", "test", "", nil)
	}

	// Empty ref = global sweep; negative duration = cutoff in future = prunes all
	emptyRef := ScopeRef{}
	n, err := SweepScopedByAge(b, emptyRef, -time.Second, false)
	if err != nil {
		t.Fatalf("SweepScopedByAge (global): %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 global rows pruned, got %d", n)
	}
	total, _ := b.CountAll()
	if total != 0 {
		t.Errorf("expected 0 remaining after global sweep, got %d", total)
	}
}

// TestBL386_ScopedSweep_DryRun verifies dry-run mode counts without deleting.
func TestBL386_ScopedSweep_DryRun_DoesNotDelete(t *testing.T) {
	b := newScopeTestStore(t)

	sessRef := ScopeRef{Scope: ScopeSessionLocal, Project: "/dryrun-proj", SessionID: "dryrun-sess"}
	sessDir, sessRole, _ := sessRef.Resolve()
	_, _ = b.Save(sessDir, "dryrun session memory A", "", sessRole, "dryrun-sess", nil)
	_, _ = b.Save(sessDir, "dryrun session memory B", "", sessRole, "dryrun-sess", nil)

	count, err := SweepScopedByAge(b, sessRef, -time.Second, true) // dry_run=true, negative = prune all
	if err != nil {
		t.Fatalf("SweepScopedByAge dry-run: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 would-be-pruned in dry run, got %d", count)
	}

	// Nothing deleted
	remaining, _ := b.ListByRole(sessDir, sessRole, 100)
	if len(remaining) != 2 {
		t.Errorf("expected 2 rows still present after dry run, got %d", len(remaining))
	}
}

// silence unused import for time-gated sweep test
var _ = time.Duration(0)
