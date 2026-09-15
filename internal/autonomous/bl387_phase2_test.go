// BL387 Phase 2 — decomposer context enrichment + cross-PRD seeding.
//
// TC-1: Decompose calls memoryContextFn with correct args when fn is set
// TC-2: Decompose does not panic when memoryContextFn returns empty string
// TC-3: Decompose does not panic when memoryContextFn is nil
// TC-4: cross-PRD seed fires for each FromPRDs entry at first run
// TC-5: cross-PRD seed NOT fired when MemorySeed.Enabled=false
// TC-6: cross-PRD seed NOT fired when FromPRDs is empty
// TC-7: cross-PRD seed skipped on resume (status already PRDRunning)
// TC-8: cross-PRD seed uses MaxPerScope, defaults to 20 when 0

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// crossSeedCall records one invocation of the memoryCrossSeedFn stub.
type crossSeedCall struct {
	fromPRDID  string
	toPRDID    string
	projectDir string
	maxEntries int
	roleFilter []string
}

// contextCall records one invocation of the memoryContextFn stub.
type contextCall struct {
	projectDir string
	spec       string
	limit      int
}

// bl387Phase2Setup builds a manager + approved PRD with MemorySeed enabled and one story/task.
func bl387Phase2Setup(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("impl spec", "/proj387p2", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "P2 Story",
			Tasks: []Task{
				{Title: "P2 Task", Spec: "do the work"},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// nopSpawnVerify returns stub spawn/verify functions that succeed immediately.
func nopSpawnVerify() (SpawnFn, VerifyFn) {
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-p2-nop"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	return spawn, verify
}

// stubDecomposeFn returns a DecomposeFn that fails immediately (for tests that
// only need to reach the context-fn call, not complete decomposition).
func stubDecomposeFn() DecomposeFn {
	return func(_ DecomposeRequest) (string, error) {
		return "", fmt.Errorf("stub: no LLM in test")
	}
}

// TestBL387_Decomposer_InjectsProjectSharedContext_WhenMemoriesExist verifies
// that memoryContextFn is called during Decompose with correct projectDir and limit.
func TestBL387_Decomposer_InjectsProjectSharedContext_WhenMemoriesExist(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), stubDecomposeFn())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("some spec", "/projctx", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Status = PRDDraft
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []contextCall
	m.SetMemoryContextFn(func(_ context.Context, projectDir, spec string, limit int) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, contextCall{projectDir: projectDir, spec: spec, limit: limit})
		return "prior context from project memory", nil
	})

	// Decompose will fail on the stub LLM call; that's fine — we only care that
	// our fn was invoked before the decompose fn runs.
	_, _ = m.Decompose(prd.ID)

	mu.Lock()
	n := len(calls)
	mu.Unlock()

	if n == 0 {
		t.Error("expected memoryContextFn to be called during Decompose, got 0 calls")
	}
	if n > 0 && calls[0].projectDir != "/projctx" {
		t.Errorf("expected projectDir=/projctx, got %q", calls[0].projectDir)
	}
	if n > 0 && calls[0].limit != 15 {
		t.Errorf("expected limit=15, got %d", calls[0].limit)
	}
}

// TestBL387_Decomposer_NoInjection_WhenNoMemoriesExist verifies that an empty
// result from memoryContextFn does not crash and Decompose proceeds normally.
func TestBL387_Decomposer_NoInjection_WhenNoMemoriesExist(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), stubDecomposeFn())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec B", "/projempty", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Status = PRDDraft
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryContextFn(func(_ context.Context, _, _ string, _ int) (string, error) {
		called = true
		return "", nil // empty — no injection
	})

	_, _ = m.Decompose(prd.ID)

	if !called {
		t.Error("expected memoryContextFn to be called even when result is empty")
	}
}

// TestBL387_Decomposer_NoInjection_WhenContextFnNil verifies that Decompose
// works normally when memoryContextFn has not been set.
func TestBL387_Decomposer_NoInjection_WhenContextFnNil(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), stubDecomposeFn())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec C", "/projnil", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Status = PRDDraft
	_ = m.Store().SavePRD(prd)

	// No SetMemoryContextFn — must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Decompose panicked with nil contextFn: %v", r)
		}
	}()
	_, _ = m.Decompose(prd.ID)
}

// TestBL387_CrossPRDSeed_SeedsFromListedPRDs_AtFirstRun verifies that
// memoryCrossSeedFn is called once per FromPRDs entry at first task spawn.
func TestBL387_CrossPRDSeed_SeedsFromListedPRDs_AtFirstRun(t *testing.T) {
	m, prd := bl387Phase2Setup(t)
	prd.MemorySeed.FromPRDs = []string{"prd-aaa", "prd-bbb"}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []crossSeedCall
	m.SetMemoryCrossSeedFn(func(_ context.Context, fromID, toID, projectDir string, maxEntries int, roleFilter []string) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, crossSeedCall{
			fromPRDID:  fromID,
			toPRDID:    toID,
			projectDir: projectDir,
			maxEntries: maxEntries,
			roleFilter: roleFilter,
		})
		return nil
	})

	spawn, verify := nopSpawnVerify()
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 cross-seed calls, got %d", len(calls))
	}
	if calls[0].fromPRDID != "prd-aaa" {
		t.Errorf("call[0] fromPRDID=%q, want prd-aaa", calls[0].fromPRDID)
	}
	if calls[1].fromPRDID != "prd-bbb" {
		t.Errorf("call[1] fromPRDID=%q, want prd-bbb", calls[1].fromPRDID)
	}
	for i, c := range calls {
		if c.toPRDID != prd.ID {
			t.Errorf("call[%d] toPRDID=%q, want %q", i, c.toPRDID, prd.ID)
		}
		if c.maxEntries != 10 {
			t.Errorf("call[%d] maxEntries=%d, want 10", i, c.maxEntries)
		}
	}
}

// TestBL387_CrossPRDSeed_NoSeed_WhenMemorySeedDisabled verifies that
// memoryCrossSeedFn is NOT called when MemorySeed.Enabled=false.
func TestBL387_CrossPRDSeed_NoSeed_WhenMemorySeedDisabled(t *testing.T) {
	m, prd := bl387Phase2Setup(t)
	prd.MemorySeed.Enabled = false
	prd.MemorySeed.FromPRDs = []string{"prd-zzz"}
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryCrossSeedFn(func(_ context.Context, _, _, _ string, _ int, _ []string) error {
		called = true
		return nil
	})

	spawn, verify := nopSpawnVerify()
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	if called {
		t.Error("memoryCrossSeedFn should NOT be called when MemorySeed.Enabled=false")
	}
}

// TestBL387_CrossPRDSeed_NoSeed_WhenFromPRDsEmpty verifies that
// memoryCrossSeedFn is NOT called when FromPRDs is empty.
func TestBL387_CrossPRDSeed_NoSeed_WhenFromPRDsEmpty(t *testing.T) {
	m, prd := bl387Phase2Setup(t)
	// FromPRDs is already empty from bl387Phase2Setup
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryCrossSeedFn(func(_ context.Context, _, _, _ string, _ int, _ []string) error {
		called = true
		return nil
	})

	spawn, verify := nopSpawnVerify()
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	if called {
		t.Error("memoryCrossSeedFn should NOT be called when FromPRDs is empty")
	}
}

// TestBL387_CrossPRDSeed_SkipsOnResume_WhenAlreadyRunning verifies that
// memoryCrossSeedFn is NOT called when PRD status is already PRDRunning.
func TestBL387_CrossPRDSeed_SkipsOnResume_WhenAlreadyRunning(t *testing.T) {
	m, prd := bl387Phase2Setup(t)
	prd.MemorySeed.FromPRDs = []string{"prd-resume"}
	prd.Status = PRDRunning // simulate resume — not a first run
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryCrossSeedFn(func(_ context.Context, _, _, _ string, _ int, _ []string) error {
		called = true
		return nil
	})

	spawn, verify := nopSpawnVerify()
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	if called {
		t.Error("memoryCrossSeedFn should NOT be called on resume (status=PRDRunning)")
	}
}

// TestBL387_CrossPRDSeed_UsesMaxPerScope_DefaultsTo20 verifies that when
// MaxPerScope=0 the cross-seed call uses the default of 20.
func TestBL387_CrossPRDSeed_UsesMaxPerScope_DefaultsTo20(t *testing.T) {
	m, prd := bl387Phase2Setup(t)
	prd.MemorySeed.MaxPerScope = 0 // force default
	prd.MemorySeed.FromPRDs = []string{"prd-default"}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	maxSeen := -1
	m.SetMemoryCrossSeedFn(func(_ context.Context, _, _, _ string, maxEntries int, _ []string) error {
		mu.Lock()
		defer mu.Unlock()
		maxSeen = maxEntries
		return nil
	})

	spawn, verify := nopSpawnVerify()
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	mu.Lock()
	defer mu.Unlock()
	if maxSeen != 20 {
		t.Errorf("expected default maxEntries=20, got %d", maxSeen)
	}
}
