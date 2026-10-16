// BL407 Phase 2 — integration test proving Manager.Run actually wires
// handleWorktreeCompletion in on PRDCompleted. Runs synchronously
// within Run() (deliberately, see executor.go's comment at the call
// site — avoids a real data race a background goroutine would
// introduce), so this test asserts directly on Run()'s return.
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

func TestRun_OpensPRAfterWorktreePRDCompletesWithAutoPR(t *testing.T) {
	// Deliberately NOT using gitCompletionFixture here: it pushes its
	// own independent commit history (via its own internal
	// scratchGitRepo call) to a bare remote's main before returning.
	// This test then used to create a SECOND, unrelated scratchGitRepo
	// and push IT to that same already-seeded remote/main -- the two
	// histories only "merge" without conflict when both commits land
	// in the same wall-clock second (git commit timestamps have 1s
	// resolution, so identical tree+author+committer+message+parent
	// hashes identically) -- true locally almost always, but a real,
	// reproducible "fetch first" rejection under CI's slower/loaded
	// runner once the two calls straddle a second boundary. Found live
	// (BL403 Phase 2's merge): failed CI 3 times running the full
	// suite, passed 8/8 locally in isolation every time. Fix: this
	// test gets its own freshly-created, never-pre-seeded bare remote.
	bareRemote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", bareRemote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	baseRepo := scratchGitRepo(t)
	run := func(args ...string) {
		if out, err := runGitIn(baseRepo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("remote", "add", "origin", bareRemote)
	run("push", "-u", "origin", "main")

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := &fakeOpenPRProvider{url: "https://github.com/x/y/pull/99"}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd, err := m.CreatePRD("spec", "", "", "", "") // empty ProjectDir -> worktree mode
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Git.AutoPR = true
	prd.Title = "Integration test PRD"
	_ = m.Store().SavePRD(prd)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Git.AutoPR = true // SetStories replaced the PRD; re-apply
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	worktreePath := got.ProjectDir
	if worktreePath == "" {
		t.Fatal("worktree was never created")
	}

	final, _ := m.Store().GetPRD(prd.ID)
	if final.Git.PRURL != "https://github.com/x/y/pull/99" {
		t.Errorf("Git.PRURL = %q, want the fake's URL", final.Git.PRURL)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 OpenPR call, got %d", len(fake.calls))
	}
	if _, serr := os.Stat(worktreePath); !os.IsNotExist(serr) {
		t.Errorf("worktree at %s should be removed after a successful completion", worktreePath)
	}
}

func TestRun_NoAutoPRLeavesWorktreeAndNeverCallsOpenPR(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd, _ := m.CreatePRD("spec", "", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved // Git.AutoPR left false (default)
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	if len(fake.calls) != 0 {
		t.Error("OpenPR must never be called when Git.AutoPR is false")
	}
	if _, serr := os.Stat(got.ProjectDir); serr != nil {
		t.Errorf("worktree must survive when AutoPR is off (operator never asked for cleanup): %v", serr)
	}
}
