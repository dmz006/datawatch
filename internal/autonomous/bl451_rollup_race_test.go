package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newBL451Store(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

func TestBL451RevSavePRDBumpsMonotonically(t *testing.T) {
	s, _ := newBL451Store(t)
	p, err := s.CreatePRD("spec", "/tmp/x", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := p.Rev
	for i := 0; i < 3; i++ {
		if err := s.SavePRD(p); err != nil {
			t.Fatal(err)
		}
		if p.Rev != prev+1 {
			t.Fatalf("rev = %d, want %d", p.Rev, prev+1)
		}
		prev = p.Rev
	}
	if r, ok := s.PRDRev(p.ID); !ok || r != prev {
		t.Fatalf("PRDRev = %d,%v want %d", r, ok, prev)
	}
}

func TestBL451RevCommitSucceedsAtCurrentRev(t *testing.T) {
	s, _ := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { p.Status = PRDCompleted; return true })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPRD(p.ID)
	if got.Status != PRDCompleted || got.Rev != rev+1 {
		t.Fatalf("status=%s rev=%d", got.Status, got.Rev)
	}
}

func TestBL451RevCommitStaleWhenRevMoved(t *testing.T) {
	s, _ := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	before := p.Status
	_ = s.SavePRD(p)
	called := false
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { called = true; p.Status = PRDCompleted; return true })
	if !errors.Is(err, ErrPRDStale) {
		t.Fatalf("err = %v, want ErrPRDStale", err)
	}
	if called {
		t.Fatal("mutate called on stale rev")
	}
	if got, _ := s.GetPRD(p.ID); got.Status != before {
		t.Fatalf("status changed to %s", got.Status)
	}
}

func TestBL451RevCommitMutateFalseDoesNotPersist(t *testing.T) {
	s, dir := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	rev := p.Rev
	path := filepath.Join(dir, "autonomous", "prds.jsonl")
	before, _ := os.ReadFile(path)
	err := s.CommitPRDIfRev(p.ID, rev, func(p *PRD) bool { return false })
	if !errors.Is(err, ErrPRDStale) {
		t.Fatalf("err = %v, want ErrPRDStale", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("store persisted despite mutate=false")
	}
	if r, _ := s.PRDRev(p.ID); r != rev {
		t.Fatalf("rev moved to %d", r)
	}
}

func TestBL451RevCommitMissingPRD(t *testing.T) {
	s, _ := newBL451Store(t)
	if err := s.CommitPRDIfRev("nope", 0, func(*PRD) bool { return true }); err == nil {
		t.Fatal("expected error for missing PRD")
	}
}

func TestBL451RevPersistsAcrossReload(t *testing.T) {
	s, dir := newBL451Store(t)
	p, _ := s.CreatePRD("spec", "/tmp/x", "", "", "")
	_ = s.SavePRD(p)
	want := p.Rev
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := s2.PRDRev(p.ID); !ok || r != want {
		t.Fatalf("reloaded rev = %d,%v want %d", r, ok, want)
	}
}

func TestBL451RevLegacyJSONLoadsAsZero(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "autonomous")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"legacy1","spec":"old","status":"draft","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "prds.jsonl"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := s.PRDRev("legacy1"); !ok || r != 0 {
		t.Fatalf("legacy rev = %d,%v want 0,true", r, ok)
	}
}

// --- BL451 completion-rollup race (2026-10-15 incident) ---

// bl451RunningPRD stores a PRD whose single task is already completed
// and whose status is running, so Run() goes straight to the rollup.
func bl451RunningPRD(t *testing.T, m *Manager, guardrails []string, taskStatus TaskStatus) (*PRD, string) {
	t.Helper()
	prd := ciApprovedPRD(t, m, ciProjectDir(t), guardrails)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.Story[0].Tasks[0].Status = taskStatus
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return prd, prd.Story[0].Tasks[0].ID
}

func bl451HasDecision(p *PRD, kind string) bool {
	for _, d := range p.Decisions {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// bl451RaceRun starts Run, waits for the slow seam to be entered,
// resets the task, releases the seam and asserts the reset survived.
func bl451RaceRun(t *testing.T, m *Manager, prdID, taskID string, entered, release chan struct{}) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- m.Run(context.Background(), prdID, fakeSpawn, fakeVerify) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("slow seam never entered")
	}
	if _, err := m.ResetTask(prdID, taskID, "test", true); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	p, _ := m.Store().GetPRD(prdID)
	if p.Status != PRDRunning {
		t.Fatalf("status = %s, want running (reset must win)", p.Status)
	}
	if st := p.Story[0].Tasks[0].Status; isTaskTerminal(st) {
		t.Fatalf("task status = %q, want pending", st)
	}
	if !bl451HasDecision(p, "rollup_aborted_stale") {
		t.Fatal("missing rollup_aborted_stale decision")
	}
}

func TestBL451RollupRaceResetDuringCIWatch(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "success") }, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.mu.Lock()
	m.ciGitFn = func(_ context.Context, _ string) (string, string, bool, error) {
		once.Do(func() { close(entered) })
		<-release
		return ciTestSHA, ciTestOrigin, true, nil
	}
	m.mu.Unlock()
	prd, taskID := bl451RunningPRD(t, m, []string{"ci-watch"}, TaskCompleted)
	bl451RaceRun(t, m, prd.ID, taskID, entered, release)
}

func TestBL451RollupRaceResetDuringRulesReviewBlock(t *testing.T) {
	cfg := DefaultConfig()
	// v9.10.18 — the resolver refuses to launch the (slow) judge fn
	// with no configured model, so the race's slow seam would never be
	// entered; declare the model the fn stands in for.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.SetGuardrail(func(_ context.Context, _ GuardrailInvocation) (GuardrailVerdict, error) {
		once.Do(func() { close(entered) })
		<-release
		return GuardrailVerdict{Outcome: "block", Category: "policy", Summary: "nope"}, nil
	})
	prd, taskID := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	bl451RaceRun(t, m, prd.ID, taskID, entered, release)
}

// PRDFailed and project-rules PRDBlocked have no slow step of their
// own; cover them by moving Rev between capture and commit.
func TestBL451RollupCommitStaleRev(t *testing.T) {
	for _, status := range []PRDStatus{PRDFailed, PRDBlocked, PRDCompleted} {
		m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
		prd, _ := bl451RunningPRD(t, m, nil, TaskFailed)
		rev, _ := m.Store().PRDRev(prd.ID)
		_ = m.Store().SavePRD(prd) // concurrent writer
		if _, ok := m.commitRollupStatus(prd, PRDRunning, rev, status, "test", ""); ok {
			t.Fatalf("%s: commit succeeded on stale rev", status)
		}
		p, _ := m.Store().GetPRD(prd.ID)
		if p.Status != PRDRunning || !bl451HasDecision(p, "rollup_aborted_stale") {
			t.Fatalf("%s: status=%s decision=%v", status, p.Status, bl451HasDecision(p, "rollup_aborted_stale"))
		}
	}
}

// Same rev, but a task went non-terminal (reset already applied):
// the under-lock recheck alone must refuse.
func TestBL451RollupCommitRechecksTasks(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	prd, _ := bl451RunningPRD(t, m, nil, TaskCompleted)
	prd.Story[0].Tasks[0].Status = TaskPending
	_ = m.Store().SavePRD(prd)
	rev, _ := m.Store().PRDRev(prd.ID)
	if _, ok := m.commitRollupStatus(prd, PRDRunning, rev, PRDFailed, "test", ""); ok {
		t.Fatal("commit succeeded with a non-terminal task")
	}
}

func TestBL451RollupNoRaceCompletes(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "success") }, nil)
	prd, _ := bl451RunningPRD(t, m, []string{"ci-watch"}, TaskCompleted)
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatal(err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if p.Status != PRDCompleted || bl451HasDecision(p, "rollup_aborted_stale") {
		t.Fatalf("status=%s", p.Status)
	}
}

func TestBL451RollupNoRaceFailed(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	prd, _ := bl451RunningPRD(t, m, nil, TaskFailed)
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.Store().GetPRD(prd.ID); p.Status != PRDFailed {
		t.Fatalf("status=%s", p.Status)
	}
}
