// Coverage tests for the decompose give-up/guard branches that the
// behavior-focused suites never took: guard clauses and worktree errors on
// BOTH entry points (Decompose / decomposeStreamingCore), the streaming
// core's retry-then-fail and fail-tail branches (LLM error, parse error,
// zero-stories, verification-script scope, depends_on), the memory-context
// success path, the DecomposeStreaming story-callback yielding, and every
// arm of failDecomposeAttempt / checkPersistedDecompose.
//
// decomposeStreamingCore is a deliberate drifted duplicate of Decompose()
// by design history — see their own comments — so each branch is asserted
// on the core path, and the shared Decompose-side branches that only
// Decompose() has (planning-mark save failure, scope-retry continue) are
// asserted there too.
package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newDecomposeCovManager wires a fresh manager + draft PRD. projectDir
// stays "" (no worktree, no verification-script gate) unless a test needs
// a tooled PRD, in which case it passes a non-empty dir.
func newDecomposeCovManager(t *testing.T, projectDir string, cfg Config, fn DecomposeFn) (*Manager, *PRD) {
	t.Helper()
	if cfg.RetryPolicy == nil {
		cfg.RetryPolicy = map[string]int{}
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("coverage spec", projectDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return m, prd
}

func requireDecomposeFailureState(t *testing.T, m *Manager, prdID, wantSource, wantDetailSub string) {
	t.Helper()
	stored, ok := m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s missing from store", prdID)
	}
	if stored.Status != PRDDraft {
		t.Fatalf("stored status = %q, want %q", stored.Status, PRDDraft)
	}
	fr := stored.LastDecomposeFailure
	if fr == nil {
		t.Fatalf("LastDecomposeFailure is nil, want source %q", wantSource)
	}
	if fr.Source != wantSource {
		t.Errorf("LastDecomposeFailure.Source = %q, want %q", fr.Source, wantSource)
	}
	if wantDetailSub != "" && !strings.Contains(fr.Detail, wantDetailSub) {
		t.Errorf("LastDecomposeFailure.Detail = %q, want substring %q", fr.Detail, wantDetailSub)
	}
}

func requireDecomposeSucceeded(t *testing.T, m *Manager, prdID string) {
	t.Helper()
	stored, ok := m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s missing from store", prdID)
	}
	if stored.Status != PRDNeedsReview {
		t.Fatalf("stored status = %q, want %q", stored.Status, PRDNeedsReview)
	}
	if err := m.checkPersistedDecompose(prdID); err != nil {
		t.Fatalf("persisted decompose check: %v", err)
	}
}

// --- Guard clauses on both entry points (manager.go ~1113-1120, 1320-1327) ---

func TestDecompose_GuardClauses(t *testing.T) {
	okFn := func(DecomposeRequest) (string, error) { return persistFailureValidPlan, nil }
	m, prd := newDecomposeCovManager(t, "", DefaultConfig(), okFn)

	if _, err := m.Decompose("no-such-prd"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Decompose(missing prd) err = %v, want not-found", err)
	}
	if _, err := m.decomposeStreamingCore("no-such-prd", nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("decomposeStreamingCore(missing prd) err = %v, want not-found", err)
	}

	m.decompose = nil
	if _, err := m.Decompose(prd.ID); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Decompose(nil decompose) err = %v, want not-configured", err)
	}
	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("decomposeStreamingCore(nil decompose) err = %v, want not-configured", err)
	}
	m.decompose = okFn

	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	stored.IsTemplate = true
	if err := m.store.SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if _, err := m.Decompose(prd.ID); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("Decompose(template) err = %v, want template refusal", err)
	}
	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("decomposeStreamingCore(template) err = %v, want template refusal", err)
	}
}

// --- Worktree setup failure (manager.go 1127-1128, 1334-1335) ---

func TestDecompose_WorktreeSetupErrorFailsBothPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = filepath.Join(t.TempDir(), "missing", "base-repo")
	m, prd := newDecomposeCovManager(t, "", cfg, func(DecomposeRequest) (string, error) {
		t.Fatal("decompose fn must not be reached when worktree setup fails")
		return "", nil
	})

	if _, err := m.Decompose(prd.ID); err == nil || !strings.Contains(err.Error(), "worktree setup") {
		t.Fatalf("Decompose err = %v, want worktree setup failure", err)
	}
	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil || !strings.Contains(err.Error(), "worktree setup") {
		t.Fatalf("decomposeStreamingCore err = %v, want worktree setup failure", err)
	}
}

// --- Scope (verification-script) retry: Decompose continue, core both arms ---

func TestDecompose_ScopeRetryContinuesAndSucceeds(t *testing.T) {
	var mu sync.Mutex
	var calls int
	var secondPrompt string
	fn := func(req DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		n := calls
		if n == 2 {
			secondPrompt = req.Spec
		}
		mu.Unlock()
		if n == 1 {
			return planRespFilesNoScript, nil // plans files, no script → scope reject
		}
		return planRespFilesWithScript, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	m, prd := newDecomposeCovManager(t, "/p", cfg, fn)

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated == nil || updated.Status != PRDNeedsReview {
		t.Fatalf("status = %v, want needs_review", updated)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2 (scope retry)", calls)
	}
	if !strings.Contains(secondPrompt, "no verification_script") {
		t.Errorf("retry prompt does not carry the scope failure hint: %q", secondPrompt)
	}
}

func TestDecomposeStreaming_CoreScopeRetrySucceeds(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(req DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return planRespFilesNoScript, nil
		}
		return planRespFilesWithScript, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	m, prd := newDecomposeCovManager(t, "/p", cfg, fn)

	updated, err := m.decomposeStreamingCore(prd.ID, nil)
	if err != nil {
		t.Fatalf("decomposeStreamingCore: %v", err)
	}
	if updated == nil || updated.Status != PRDNeedsReview {
		t.Fatalf("updated = %v, want needs_review", updated)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2", calls)
	}
	requireDecomposeSucceeded(t, m, prd.ID)
}

func TestDecomposeStreaming_CoreScopeBudgetExhaustFails(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return planRespFilesNoScript, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	m, prd := newDecomposeCovManager(t, "/p", cfg, fn)

	_, err := m.decomposeStreamingCore(prd.ID, nil)
	if err == nil || !strings.Contains(err.Error(), "no verification_script") {
		t.Fatalf("err = %v, want scope (no verification_script) failure", err)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2 (retry then give-up)", calls)
	}
	requireDecomposeFailureState(t, m, prd.ID, "verification-script-coverage", "no verification_script")
}

// --- Zero-story guard on the streaming core ---

const zeroStoryPlan = `{"title":"Empty","stories":[]}`

func TestDecomposeStreaming_CoreZeroStories_RetrySucceeds(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return zeroStoryPlan, nil
		}
		return persistFailureValidPlan, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	m, prd := newDecomposeCovManager(t, "", cfg, fn)

	updated, err := m.decomposeStreamingCore(prd.ID, nil)
	if err != nil {
		t.Fatalf("decomposeStreamingCore: %v", err)
	}
	if updated == nil || updated.Status != PRDNeedsReview {
		t.Fatalf("updated = %v, want needs_review", updated)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2 (zero-story retry)", calls)
	}
	requireDecomposeSucceeded(t, m, prd.ID)
}

func TestDecomposeStreaming_CoreZeroStories_BudgetExhaustFails(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return zeroStoryPlan, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 0}
	m, prd := newDecomposeCovManager(t, "", cfg, fn)

	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil {
		t.Fatal("want an error when the plan has zero stories and no budget")
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (no budget)", calls)
	}
	requireDecomposeFailureState(t, m, prd.ID, "decompose-zero-stories", "no stories")
}

// --- depends_on rejection on the streaming core ---

const unresolvableDependsOnPlan = `{"title":"Ship it","stories":[{"title":"A","tasks":[{"title":"T1","spec":"do x"},{"title":"T2","spec":"do y","depends_on":["s9-t9"]}]}]}`

func TestDecomposeStreaming_CoreDependsOn_RetrySucceeds(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return unresolvableDependsOnPlan, nil
		}
		return persistFailureValidPlan, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	m, prd := newDecomposeCovManager(t, "", cfg, fn)

	updated, err := m.decomposeStreamingCore(prd.ID, nil)
	if err != nil {
		t.Fatalf("decomposeStreamingCore: %v", err)
	}
	if updated == nil || updated.Status != PRDNeedsReview {
		t.Fatalf("updated = %v, want needs_review", updated)
	}
	if calls != 2 {
		t.Fatalf("decompose calls = %d, want 2 (depends-on retry)", calls)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 1 {
		t.Fatalf("stored stories = %d, want 1", n)
	}
	requireDecomposeSucceeded(t, m, prd.ID)
}

func TestDecomposeStreaming_CoreDependsOn_BudgetExhaustFails(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return unresolvableDependsOnPlan, nil
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 0}
	m, prd := newDecomposeCovManager(t, "", cfg, fn)

	if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil {
		t.Fatal("want an error for unresolvable depends_on with no budget")
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (no budget)", calls)
	}
	if n := len(storedStoriesFor(m, prd.ID)); n != 0 {
		t.Fatalf("stored stories = %d, want 0", n)
	}
	requireDecomposeFailureState(t, m, prd.ID, "decompose-depends-on", "s9-t9")
}

// --- LLM-call failure give-up on the streaming core (1402-1403) ---

func TestDecomposeStreaming_CoreLLMFails_BudgetExhaustFails(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fn := func(DecomposeRequest) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return "", errors.New("model endpoint down")
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"infra": 0}
	m, prd := newDecomposeCovManager(t, "", cfg, fn)

	_, err := m.decomposeStreamingCore(prd.ID, nil)
	if err == nil || !strings.Contains(err.Error(), "LLM plan") {
		t.Fatalf("err = %v, want LLM plan failure", err)
	}
	if calls != 1 {
		t.Fatalf("decompose calls = %d, want 1 (no infra budget)", calls)
	}
	requireDecomposeFailureState(t, m, prd.ID, "decompose-llm", "model endpoint down")
}

// --- Parse failure give-up on the streaming core (1412-1414) ---

func TestDecomposeStreaming_CoreParseFails_Terminal(t *testing.T) {
	fn := func(DecomposeRequest) (string, error) { return "{not valid json", nil }
	m, prd := newDecomposeCovManager(t, "", DefaultConfig(), fn)

	_, err := m.decomposeStreamingCore(prd.ID, nil)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("err = %v, want parse failure", err)
	}
	requireDecomposeFailureState(t, m, prd.ID, "decompose-parse", "")
	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if got := stored.LastDecomposeFailure.Category; got != "invalid-response" {
		t.Errorf("failure category = %q, want invalid-response", got)
	}
}

// --- Memory-context success path on the streaming core (1362-1364) ---

func TestDecomposeStreaming_CoreMemoryContextInjected(t *testing.T) {
	var mu sync.Mutex
	var ctxCalls int
	m, prd := newDecomposeCovManager(t, "/p", DefaultConfig(), func(DecomposeRequest) (string, error) {
		return persistFailureValidPlan, nil
	})
	m.SetMemoryContextFn(func(_ context.Context, projectDir, query string, topK int) (string, error) {
		mu.Lock()
		ctxCalls++
		mu.Unlock()
		if projectDir != "/p" {
			t.Errorf("memory context projectDir = %q, want /p", projectDir)
		}
		return "prior context block", nil
	})

	var gotSpec string
	fncall := func(req DecomposeRequest) (string, error) {
		gotSpec = req.Spec
		return persistFailureValidPlan, nil
	}
	m.decompose = fncall

	if _, err := m.decomposeStreamingCore(prd.ID, nil); err != nil {
		t.Fatalf("decomposeStreamingCore: %v", err)
	}
	if ctxCalls != 1 {
		t.Fatalf("memory context calls = %d, want 1", ctxCalls)
	}
	if !strings.Contains(gotSpec, "prior context block") {
		t.Errorf("planning prompt does not contain the memory context: %q", gotSpec)
	}
	requireDecomposeSucceeded(t, m, prd.ID)
}

// --- DecomposeStreaming yields each story through the callback (1310, 1498) ---

func TestDecomposeStreaming_YieldsEachStory(t *testing.T) {
	twoStoryPlan := `{"title":"Two","stories":[{"title":"A","tasks":[{"title":"T1","spec":"a"},{"title":"T2","spec":"b"}]},{"title":"B","tasks":[{"title":"T3","spec":"c"}]}]}`
	m, prd := newDecomposeCovManager(t, "", DefaultConfig(), func(DecomposeRequest) (string, error) {
		return twoStoryPlan, nil
	})

	type got struct {
		index int
		total int
		title string
	}
	var mu sync.Mutex
	var yielded []got
	updated, err := m.DecomposeStreaming(prd.ID, func(index, total int, story any) {
		s, ok := story.(Story)
		if !ok {
			t.Errorf("story yielded as %T, want Story", story)
			return
		}
		mu.Lock()
		yielded = append(yielded, got{index, total, s.Title})
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("DecomposeStreaming: %v", err)
	}
	upd, ok := updated.(*PRD)
	if !ok || upd.Status != PRDNeedsReview {
		t.Fatalf("updated = %v, want needs_review PRD", updated)
	}
	if len(yielded) != 2 {
		t.Fatalf("yielded %d stories, want 2", len(yielded))
	}
	want := []got{{0, 2, "A"}, {1, 2, "B"}}
	for i := range want {
		if yielded[i] != want[i] {
			t.Errorf("yielded[%d] = %+v, want %+v", i, yielded[i], want[i])
		}
	}
}

// --- failDecomposeAttempt: non-NotExist artifact stat error + persist arm ---

func TestFailDecomposeAttempt_ArtifactStatErrorStillRecords(t *testing.T) {
	dataDir := t.TempDir()
	m, err := NewManager(dataDir, DefaultConfig(), func(DecomposeRequest) (string, error) {
		return "", errors.New("unused")
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	// Make the PRD's scratch slot a FILE so MarkDecomposeOutputArtifactFailed's
	// stat returns ENOTDIR — a non-NotExist error — which must be logged,
	// never propagated (retry_policy.go 242-243).
	scratchSlot := filepath.Join(dataDir, "autonomous", "scratch", prd.ID)
	if err := os.MkdirAll(filepath.Dir(scratchSlot), 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	if err := os.WriteFile(scratchSlot, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write scratch blocker: %v", err)
	}

	prd.Status = PRDPlanning
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// LastDecomposeFailure is nil — the give-up tail must record the reason
	// itself, taking the decompose-persist arm (infra + retryable).
	m.failDecomposeAttempt(prd, "decompose-persist", errors.New("disk full"))

	stored, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store")
	}
	if stored.Status != PRDDraft {
		t.Fatalf("stored status = %q, want %q", stored.Status, PRDDraft)
	}
	fr := stored.LastDecomposeFailure
	if fr == nil {
		t.Fatal("LastDecomposeFailure is nil, want a recorded reason")
	}
	if fr.Source != "decompose-persist" {
		t.Errorf("source = %q, want decompose-persist", fr.Source)
	}
	if fr.Category != "infra" {
		t.Errorf("category = %q, want infra", fr.Category)
	}
	if !fr.Retryable {
		t.Error("retryable = false, want true for a persist failure")
	}
	if fr.Detail != "disk full" {
		t.Errorf("detail = %q, want %q", fr.Detail, "disk full")
	}
}

// --- checkPersistedDecompose: every branch (retry_policy.go 284-290) ---

func TestCheckPersistedDecompose_Branches(t *testing.T) {
	m, prd := newDecomposeCovManager(t, "", DefaultConfig(), func(DecomposeRequest) (string, error) {
		return persistFailureValidPlan, nil
	})

	if err := m.checkPersistedDecompose("no-such-prd"); err == nil || !strings.Contains(err.Error(), "missing from store") {
		t.Fatalf("checkPersistedDecompose(missing) err = %v, want missing-from-store", err)
	}
	if err := m.checkPersistedDecompose(prd.ID); err == nil || !strings.Contains(err.Error(), "persisted decompose") {
		t.Fatalf("checkPersistedDecompose(no stories) err = %v, want persisted-decompose failure", err)
	}
	if err := m.store.SetStories(prd.ID, []Story{{Title: "A", Tasks: []Task{{Title: "T1", Spec: "a"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	if err := m.checkPersistedDecompose(prd.ID); err != nil {
		t.Fatalf("checkPersistedDecompose(valid) err = %v, want nil", err)
	}
}

// --- Success-tail: persisted-state check failure on BOTH paths ---
// (Decompose 1287-1290, decomposeStreamingCore 1485-1488)

// dropPersistedStoriesHook makes the FINAL SavePRD commit fine but with the
// stories gone from the store — so checkPersistedDecompose (which only
// trusts persisted state) rejects the decompose and the give-up tail runs.
func dropPersistedStoriesHook(m *Manager, prd *PRD) {
	m.store.SavePRDFailHook = func(p *PRD) bool {
		if p.ID != prd.ID {
			return false
		}
		for sid, st := range m.store.stories {
			if st.PRDID == prd.ID {
				delete(m.store.stories, sid)
			}
		}
		return false // the save itself succeeds; only the stories are lost
	}
}

func TestDecomperse_PersistedStateLostFailsBothPaths(t *testing.T) {
	t.Run("Decompose", func(t *testing.T) {
		fn := func(DecomposeRequest) (string, error) { return persistFailureValidPlan, nil }
		m, prd := newDecomposeCovManager(t, "", DefaultConfig(), fn)
		dropPersistedStoriesHook(m, prd)

		_, err := m.Decompose(prd.ID)
		if err == nil || !strings.Contains(err.Error(), "persisted decompose") {
			t.Fatalf("Decompose err = %v, want persisted-decompose failure", err)
		}
		requireDecomposeFailureState(t, m, prd.ID, "decompose-persist", "")
		stored, ok := m.store.GetPRD(prd.ID)
		if !ok {
			t.Fatal("PRD missing from store")
		}
		if stored.LastDecomposeFailure.Category != "infra" {
			t.Errorf("failure category = %q, want infra", stored.LastDecomposeFailure.Category)
		}
		if !stored.LastDecomposeFailure.Retryable {
			t.Error("failure retryable = false, want true")
		}
	})

	t.Run("decomposeStreamingCore", func(t *testing.T) {
		fn := func(DecomposeRequest) (string, error) { return persistFailureValidPlan, nil }
		m, prd := newDecomposeCovManager(t, "", DefaultConfig(), fn)
		dropPersistedStoriesHook(m, prd)

		if _, err := m.decomposeStreamingCore(prd.ID, nil); err == nil || !strings.Contains(err.Error(), "persisted decompose") {
			t.Fatalf("decomposeStreamingCore err = %v, want persisted-decompose failure", err)
		}
		requireDecomposeFailureState(t, m, prd.ID, "decompose-persist", "")
	})
}
