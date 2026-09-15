// BL387 Phase 1 — verifier memory writes + child PRD prd-shared inheritance.
//
// TC-1: verifierFn called for each issue when verify fails + MemorySeed.Enabled
// TC-2: verifierFn NOT called when verify succeeds
// TC-3: verifierFn NOT called when MemorySeed.Enabled=false
// TC-4: child PRD inherits parent prd-shared when MemorySeed.Enabled + fn set
// TC-5: child PRD does NOT inherit when MemorySeed.Enabled=false
// TC-6: child PRD scope-seed capped at 50

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// verifierCall records one invocation of the memoryVerifierFn stub.
type verifierCall struct {
	prdID      string
	projectDir string
	content    string
	role       string
}

// bl387Setup builds a manager + approved PRD with one story/task. Mirrors bl386Setup.
func bl387Setup(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/proj387", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "BL387 Story",
			Tasks: []Task{
				{Title: "BL387 Task", Spec: "implement the thing"},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// TestBL387_Verifier_WritesFindings_ToPRDShared_OnFailure verifies that
// memoryVerifierFn is called once per issue when verify() returns !OK and
// MemorySeed.Enabled=true.
func TestBL387_Verifier_WritesFindings_ToPRDShared_OnFailure(t *testing.T) {
	m, prd := bl387Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	_ = m.Store().SavePRD(prd)

	var mu sync.Mutex
	var calls []verifierCall
	m.SetMemoryVerifierFn(func(_ context.Context, prdID, projectDir, content, role string) error {
		mu.Lock()
		calls = append(calls, verifierCall{prdID, projectDir, content, role})
		mu.Unlock()
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-vf-1"}, nil
	}
	// Verifier always fails with 2 issues; executor runs up to MaxRetries, so we
	// allow it to exhaust retries and return TaskFailed.
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{
			OK:      false,
			Summary: "two problems found",
			Issues:  []string{"issue-alpha", "issue-beta"},
		}, nil
	}
	// Run will ultimately fail (retries exhausted) — we care about verifierFn calls.
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected memoryVerifierFn to be called at least once, got 0")
	}
	// Each retry emits 2 issues; at minimum one retry must have fired.
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 verifierFn calls (one per issue), got %d", len(calls))
	}
	for i, c := range calls {
		if c.prdID != prd.ID {
			t.Errorf("call[%d]: prdID mismatch: got %q, want %q", i, c.prdID, prd.ID)
		}
		if c.projectDir != prd.ProjectDir {
			t.Errorf("call[%d]: projectDir mismatch: got %q, want %q", i, c.projectDir, prd.ProjectDir)
		}
		if c.role != "verifier-finding" {
			t.Errorf("call[%d]: role mismatch: got %q, want %q", i, c.role, "verifier-finding")
		}
	}
}

// TestBL387_Verifier_NoWrite_OnSuccess verifies that memoryVerifierFn is NOT
// called when verify() returns OK=true.
func TestBL387_Verifier_NoWrite_OnSuccess(t *testing.T) {
	m, prd := bl387Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	_ = m.Store().SavePRD(prd)

	called := false
	m.SetMemoryVerifierFn(func(_ context.Context, _, _, _, _ string) error {
		called = true
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-vf-ok"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "all good"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("memoryVerifierFn must NOT be called when verify returns OK=true")
	}
}

// TestBL387_Verifier_NoWrite_WhenMemorySeedDisabled verifies that
// memoryVerifierFn is NOT called when MemorySeed.Enabled=false even if verify fails.
func TestBL387_Verifier_NoWrite_WhenMemorySeedDisabled(t *testing.T) {
	m, prd := bl387Setup(t)
	// MemorySeed.Enabled defaults to false

	called := false
	m.SetMemoryVerifierFn(func(_ context.Context, _, _, _, _ string) error {
		called = true
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-vf-disabled"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "fail", Issues: []string{"issue-x"}}, nil
	}
	_ = m.Run(context.Background(), prd.ID, spawn, verify)
	if called {
		t.Error("memoryVerifierFn must NOT be called when MemorySeed.Enabled=false")
	}
}

// scopeSeedCall records one invocation of the memoryScopeSeedFn stub.
type scopeSeedCall struct {
	fromPRDID  string
	toPRDID    string
	projectDir string
	maxEntries int
}

// TestBL387_ChildPRD_InheritsParentPRDShared_WhenSeedEnabled verifies that
// memoryScopeSeedFn is called with the parent PRD ID → child PRD ID when
// MemorySeed.Enabled=true.
func TestBL387_ChildPRD_InheritsParentPRDShared_WhenSeedEnabled(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AutoApproveChildren = true
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	parent, err := m.CreatePRD("parent spec", "/proj387b", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD parent: %v", err)
	}
	// A single child-PRD task triggers recurseChildPRD.
	_ = m.Store().SetStories(parent.ID, []Story{
		{
			Title: "Story with child PRD",
			Tasks: []Task{
				{Title: "Spawn child", Spec: "child spec", SpawnPRD: true},
			},
		},
	})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	var mu sync.Mutex
	var calls []scopeSeedCall
	m.SetMemoryScopeSeedFn(func(_ context.Context, fromPRDID, toPRDID, projectDir string, maxEntries int) error {
		mu.Lock()
		calls = append(calls, scopeSeedCall{fromPRDID, toPRDID, projectDir, maxEntries})
		mu.Unlock()
		return nil
	})

	decompose := func(req DecomposeRequest) (string, error) {
		// Return a minimal story JSON so the child PRD can decompose.
		return `{"stories":[{"title":"Child Story","tasks":[{"title":"Child Task","spec":"do child thing"}]}]}`, nil
	}
	m.decompose = decompose

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-child-seed"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	_ = m.Run(context.Background(), parent.ID, spawn, verify)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("expected memoryScopeSeedFn to be called for child PRD, got 0 calls")
	}
	if calls[0].fromPRDID != parent.ID {
		t.Errorf("fromPRDID: got %q, want %q", calls[0].fromPRDID, parent.ID)
	}
	if calls[0].toPRDID == "" || calls[0].toPRDID == parent.ID {
		t.Errorf("toPRDID should be a distinct child PRD ID, got %q", calls[0].toPRDID)
	}
	if calls[0].projectDir != parent.ProjectDir {
		t.Errorf("projectDir: got %q, want %q", calls[0].projectDir, parent.ProjectDir)
	}
}

// TestBL387_ChildPRD_NoInheritance_WhenSeedDisabled verifies that
// memoryScopeSeedFn is NOT called when parent's MemorySeed.Enabled=false.
func TestBL387_ChildPRD_NoInheritance_WhenSeedDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AutoApproveChildren = true
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	parent, err := m.CreatePRD("parent spec", "/proj387c", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD parent: %v", err)
	}
	_ = m.Store().SetStories(parent.ID, []Story{
		{
			Title: "Story with child PRD",
			Tasks: []Task{
				{Title: "Spawn child", Spec: "child spec", SpawnPRD: true},
			},
		},
	})
	parent, _ = m.Store().GetPRD(parent.ID)
	// MemorySeed.Enabled = false (default)
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	called := false
	m.SetMemoryScopeSeedFn(func(_ context.Context, _, _, _ string, _ int) error {
		called = true
		return nil
	})

	decompose := func(req DecomposeRequest) (string, error) {
		return `{"stories":[{"title":"Child Story","tasks":[{"title":"Child Task","spec":"do child thing"}]}]}`, nil
	}
	m.decompose = decompose

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-child-noseed"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	_ = m.Run(context.Background(), parent.ID, spawn, verify)

	if called {
		t.Error("memoryScopeSeedFn must NOT be called when parent MemorySeed.Enabled=false")
	}
}

// TestBL387_ChildPRD_InheritanceCappedAt50 verifies that the maxEntries
// argument passed to memoryScopeSeedFn is exactly 50.
func TestBL387_ChildPRD_InheritanceCappedAt50(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AutoApproveChildren = true
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	parent, err := m.CreatePRD("parent spec", "/proj387d", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD parent: %v", err)
	}
	_ = m.Store().SetStories(parent.ID, []Story{
		{
			Title: "Story with child PRD",
			Tasks: []Task{
				{Title: "Spawn child", Spec: "child spec", SpawnPRD: true},
			},
		},
	})
	parent, _ = m.Store().GetPRD(parent.ID)
	parent.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 200} // higher than cap
	parent.Status = PRDApproved
	_ = m.Store().SavePRD(parent)

	var maxSeen int
	m.SetMemoryScopeSeedFn(func(_ context.Context, _, _, _ string, maxEntries int) error {
		maxSeen = maxEntries
		return nil
	})

	decompose := func(req DecomposeRequest) (string, error) {
		return `{"stories":[{"title":"Child Story","tasks":[{"title":"Child Task","spec":"do child thing"}]}]}`, nil
	}
	m.decompose = decompose

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-child-cap"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	_ = m.Run(context.Background(), parent.ID, spawn, verify)

	if maxSeen != 50 {
		t.Errorf("expected maxEntries=50 (hard cap), got %d", maxSeen)
	}
}

// TestBL387_Verifier_ContainsPrefix verifies the [verifier-finding] prefix
// is present in the content passed to memoryVerifierFn.
func TestBL387_Verifier_ContainsPrefix(t *testing.T) {
	m, prd := bl387Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true}
	_ = m.Store().SavePRD(prd)

	var contents []string
	m.SetMemoryVerifierFn(func(_ context.Context, _, _, content, _ string) error {
		contents = append(contents, content)
		return nil
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-vf-prefix"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "bad", Issues: []string{"missing test", "wrong output"}}, nil
	}
	_ = m.Run(context.Background(), prd.ID, spawn, verify)

	for _, c := range contents {
		if len(c) < 18 || c[:18] != "[verifier-finding]" {
			t.Errorf("content missing [verifier-finding] prefix: %q", c)
		}
	}
	if len(contents) == 0 {
		t.Fatal("expected at least one verifierFn call")
	}
}

// TestBL387_Verifier_ErrorDoesNotAbortRetry verifies that an error from
// memoryVerifierFn is logged but does not abort the retry loop.
func TestBL387_Verifier_ErrorDoesNotAbortRetry(t *testing.T) {
	m, prd := bl387Setup(t)

	prd.MemorySeed = MemorySeedConfig{Enabled: true}
	_ = m.Store().SavePRD(prd)

	m.SetMemoryVerifierFn(func(_ context.Context, _, _, _, _ string) error {
		return fmt.Errorf("simulated memory write failure")
	})

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-vf-err"}, nil
	}
	verifyCount := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCount++
		return VerificationResult{OK: false, Summary: "bad", Issues: []string{"issue"}}, nil
	}
	// Run must complete (even if all retries fail) without returning an error from
	// the verifier write path. The PRD will be in TaskFailed state but Run returns nil.
	_ = m.Run(context.Background(), prd.ID, spawn, verify)
	if verifyCount == 0 {
		t.Error("verify was never called — retry loop must have aborted early")
	}
}
