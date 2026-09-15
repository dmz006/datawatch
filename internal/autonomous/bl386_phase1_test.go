// BL386 Phase 1 — warm-start seeding tests.
//
// TC-1: executor passes MemorySeed from PRD into SpawnRequest
// TC-2: seedFn called with correct sessionID + projectDir (project-shared context)
// TC-3: seedFn receives prdID and storyID (prd-shared / story-shared context)
// TC-4: seedFn NOT called when MemorySeed.Enabled=false
// TC-5: SetMemorySeed round-trips correctly through manager/store

package autonomous

import (
	"context"
	"sync"
	"testing"
)

// seedCall records one call to the memorySeedFn stub.
type seedCall struct {
	sessionID  string
	prdID      string
	storyID    string
	projectDir string
	cfg        MemorySeedConfig
}

// bl386Setup creates a manager + approved PRD with one story/task.
func bl386Setup(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/proj", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "BL386 Story",
			Tasks: []Task{
				{Title: "BL386 Task", Spec: "do the thing"},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// TestBL386_Executor_AutoSeeds_FromProjectShared_WhenEnabled verifies that
// seedFn is called with the spawned session ID and project dir when enabled.
func TestBL386_Executor_AutoSeeds_FromProjectShared_WhenEnabled(t *testing.T) {
	m, prd := bl386Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []seedCall
	m.SetMemorySeedFn(func(_ context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error {
		mu.Lock()
		calls = append(calls, seedCall{sessionID, prdID, storyID, projectDir, cfg})
		mu.Unlock()
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-seed-1"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected seedFn to be called at least once, got 0 calls")
	}
	if calls[0].sessionID != "sess-seed-1" {
		t.Errorf("expected sessionID=sess-seed-1, got %q", calls[0].sessionID)
	}
	if calls[0].projectDir != prd.ProjectDir {
		t.Errorf("expected projectDir=%q, got %q", prd.ProjectDir, calls[0].projectDir)
	}
	if !calls[0].cfg.Enabled {
		t.Error("expected cfg.Enabled=true in seedFn")
	}
}

// TestBL386_Executor_AutoSeeds_FromPRDShared_WhenEnabled verifies that prdID
// is forwarded to seedFn (needed for prd-shared scope lookup).
func TestBL386_Executor_AutoSeeds_FromPRDShared_WhenEnabled(t *testing.T) {
	m, prd := bl386Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 5}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []seedCall
	m.SetMemorySeedFn(func(_ context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error {
		mu.Lock()
		calls = append(calls, seedCall{sessionID, prdID, storyID, projectDir, cfg})
		mu.Unlock()
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-prd-scope"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected seedFn to be called")
	}
	if calls[0].prdID != prd.ID {
		t.Errorf("expected prdID=%q, got %q", prd.ID, calls[0].prdID)
	}
}

// TestBL386_Executor_AutoSeeds_FromStoryShared_WhenEnabled verifies that
// storyID and RoleFilter are forwarded to seedFn.
func TestBL386_Executor_AutoSeeds_FromStoryShared_WhenEnabled(t *testing.T) {
	m, prd := bl386Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true, RoleFilter: []string{"learning"}}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []seedCall
	m.SetMemorySeedFn(func(_ context.Context, sessionID, prdID, storyID, projectDir string, cfg MemorySeedConfig) error {
		mu.Lock()
		calls = append(calls, seedCall{sessionID, prdID, storyID, projectDir, cfg})
		mu.Unlock()
		return nil
	})

	var spawnedReq SpawnRequest
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawnedReq = req
		return SpawnResult{SessionID: "sess-story-scope"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected seedFn to be called")
	}
	if calls[0].storyID != spawnedReq.StoryID {
		t.Errorf("storyID mismatch: seedFn got %q, spawn got %q", calls[0].storyID, spawnedReq.StoryID)
	}
	if len(calls[0].cfg.RoleFilter) == 0 || calls[0].cfg.RoleFilter[0] != "learning" {
		t.Errorf("expected RoleFilter=[learning], got %v", calls[0].cfg.RoleFilter)
	}
}

// TestBL386_Executor_NoSeed_WhenDisabled verifies seedFn is never called when
// MemorySeed.Enabled=false (zero value).
func TestBL386_Executor_NoSeed_WhenDisabled(t *testing.T) {
	m, prd := bl386Setup(t)
	// MemorySeed.Enabled defaults to false — don't touch it

	called := false
	m.SetMemorySeedFn(func(_ context.Context, _, _, _, _ string, _ MemorySeedConfig) error {
		called = true
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-no-seed"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("seedFn must NOT be called when MemorySeed.Enabled=false")
	}
}

// TestBL386_StartSession_MemorySeed_ProxiesToScopeSeed verifies that
// SetMemorySeed round-trips correctly through the manager and store,
// confirming the data model the start_session handler relies on.
func TestBL386_StartSession_MemorySeed_ProxiesToScopeSeed(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	cfg := MemorySeedConfig{Enabled: true, MaxPerScope: 20, RoleFilter: []string{"learning", "decision"}}
	updated, err := m.SetMemorySeed(prd.ID, cfg, "test-actor")
	if err != nil {
		t.Fatalf("SetMemorySeed: %v", err)
	}
	if !updated.MemorySeed.Enabled {
		t.Error("expected MemorySeed.Enabled=true after SetMemorySeed")
	}
	if updated.MemorySeed.MaxPerScope != 20 {
		t.Errorf("expected MaxPerScope=20, got %d", updated.MemorySeed.MaxPerScope)
	}
	if len(updated.MemorySeed.RoleFilter) != 2 {
		t.Errorf("expected 2 role filters, got %d", len(updated.MemorySeed.RoleFilter))
	}
	// Verify it was persisted
	reloaded, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after SetMemorySeed")
	}
	if !reloaded.MemorySeed.Enabled {
		t.Error("MemorySeed not persisted to store")
	}
	// Verify Decision audit trail
	if len(reloaded.Decisions) == 0 {
		t.Error("expected a Decision record from SetMemorySeed")
	}
	if reloaded.Decisions[len(reloaded.Decisions)-1].Kind != "set_memory_seed" {
		t.Errorf("expected last Decision.Kind=set_memory_seed, got %q",
			reloaded.Decisions[len(reloaded.Decisions)-1].Kind)
	}
}
