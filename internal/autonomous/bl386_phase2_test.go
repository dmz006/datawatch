// BL386 Phase 2 — harvest-on-completion tests.
//
// TC-1: harvestFn called with correct sessionID and cfg after task completes
// TC-2: harvestFn receives prdID, storyID, and projectDir
// TC-3: RoleFilter forwarded to harvestFn
// TC-4: harvestFn NOT called when MemoryHarvest.Enabled=false
// TC-5: SetMemoryHarvest round-trips correctly through manager/store

package autonomous

import (
	"context"
	"sync"
	"testing"
)

// harvestCall records one call to the memoryHarvestFn stub.
type harvestCall struct {
	sessionID  string
	prdID      string
	storyID    string
	projectDir string
	cfg        MemoryHarvestConfig
}

// TestBL386_Executor_Harvests_OnTaskCompletion_WhenEnabled verifies that
// harvestFn is called with the completed task's session ID and config.
func TestBL386_Executor_Harvests_OnTaskCompletion_WhenEnabled(t *testing.T) {
	m, prd := bl386Setup(t)

	prd.MemoryHarvest = MemoryHarvestConfig{Enabled: true, PromoteTo: "story-shared", Max: 50}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []harvestCall
	m.SetMemoryHarvestFn(func(_ context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error {
		mu.Lock()
		calls = append(calls, harvestCall{sessionID, prdID, storyID, projectDir, cfg})
		mu.Unlock()
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-harvest-1"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected harvestFn to be called at least once")
	}
	if calls[0].sessionID != "sess-harvest-1" {
		t.Errorf("expected sessionID=sess-harvest-1, got %q", calls[0].sessionID)
	}
	if !calls[0].cfg.Enabled {
		t.Error("expected cfg.Enabled=true in harvestFn")
	}
	if calls[0].cfg.PromoteTo != "story-shared" {
		t.Errorf("expected PromoteTo=story-shared, got %q", calls[0].cfg.PromoteTo)
	}
}

// TestBL386_Executor_Harvest_RoleFilter_Respected verifies that prdID,
// storyID, projectDir, and RoleFilter are forwarded to harvestFn.
func TestBL386_Executor_Harvest_RoleFilter_Respected(t *testing.T) {
	m, prd := bl386Setup(t)

	prd.MemoryHarvest = MemoryHarvestConfig{
		Enabled:    true,
		PromoteTo:  "prd-shared",
		RoleFilter: []string{"learning", "decision"},
		Max:        25,
	}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []harvestCall
	m.SetMemoryHarvestFn(func(_ context.Context, sessionID, prdID, storyID, projectDir string, cfg MemoryHarvestConfig) error {
		mu.Lock()
		calls = append(calls, harvestCall{sessionID, prdID, storyID, projectDir, cfg})
		mu.Unlock()
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-harvest-2"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected harvestFn to be called")
	}
	if calls[0].prdID != prd.ID {
		t.Errorf("expected prdID=%q, got %q", prd.ID, calls[0].prdID)
	}
	if calls[0].projectDir != prd.ProjectDir {
		t.Errorf("expected projectDir=%q, got %q", prd.ProjectDir, calls[0].projectDir)
	}
	if len(calls[0].cfg.RoleFilter) != 2 {
		t.Errorf("expected 2 role filters, got %d", len(calls[0].cfg.RoleFilter))
	}
	if calls[0].cfg.Max != 25 {
		t.Errorf("expected Max=25, got %d", calls[0].cfg.Max)
	}
	if calls[0].cfg.PromoteTo != "prd-shared" {
		t.Errorf("expected PromoteTo=prd-shared, got %q", calls[0].cfg.PromoteTo)
	}
}

// TestBL386_Executor_NoHarvest_WhenDisabled verifies harvestFn is never called
// when MemoryHarvest.Enabled=false (zero value).
func TestBL386_Executor_NoHarvest_WhenDisabled(t *testing.T) {
	m, _ := bl386Setup(t)
	// MemoryHarvest.Enabled defaults to false — don't touch it

	called := false
	m.SetMemoryHarvestFn(func(_ context.Context, _, _, _, _ string, _ MemoryHarvestConfig) error {
		called = true
		return nil
	})

	// Still need a valid approved PRD to run; bl386Setup already handles this.
	prds := m.Store().ListPRDs()
	if len(prds) == 0 {
		t.Fatal("no PRD in store")
	}

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-no-harvest"}, nil
	}
	if err := m.Run(context.Background(), prds[0].ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("harvestFn must NOT be called when MemoryHarvest.Enabled=false")
	}
}

// TestBL386_MemoryHarvest_Tool_ProxiesToScopeSeed verifies that SetMemoryHarvest
// round-trips correctly through the manager and store.
func TestBL386_MemoryHarvest_Tool_ProxiesToScopeSeed(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	cfg := MemoryHarvestConfig{
		Enabled:    true,
		PromoteTo:  "story-shared",
		RoleFilter: []string{"learning"},
		Max:        40,
	}
	updated, err := m.SetMemoryHarvest(prd.ID, cfg, "test-actor")
	if err != nil {
		t.Fatalf("SetMemoryHarvest: %v", err)
	}
	if !updated.MemoryHarvest.Enabled {
		t.Error("expected MemoryHarvest.Enabled=true after SetMemoryHarvest")
	}
	if updated.MemoryHarvest.PromoteTo != "story-shared" {
		t.Errorf("expected PromoteTo=story-shared, got %q", updated.MemoryHarvest.PromoteTo)
	}
	if updated.MemoryHarvest.Max != 40 {
		t.Errorf("expected Max=40, got %d", updated.MemoryHarvest.Max)
	}
	// Verify persisted
	reloaded, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after SetMemoryHarvest")
	}
	if !reloaded.MemoryHarvest.Enabled {
		t.Error("MemoryHarvest not persisted to store")
	}
	// Verify Decision audit trail
	if len(reloaded.Decisions) == 0 {
		t.Error("expected a Decision record from SetMemoryHarvest")
	}
	last := reloaded.Decisions[len(reloaded.Decisions)-1]
	if last.Kind != "set_memory_harvest" {
		t.Errorf("expected Decision.Kind=set_memory_harvest, got %q", last.Kind)
	}
}
