// BL407 Phase 2 — integration test proving Manager.Run actually wires
// handleWorktreeCompletion in on PRDCompleted. Runs synchronously
// within Run() (deliberately, see executor.go's comment at the call
// site — avoids a real data race a background goroutine would
// introduce), so this test asserts directly on Run()'s return.
package autonomous

import (
	"context"
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

func TestRun_OpensPRAfterWorktreePRDCompletesWithAutoPR(t *testing.T) {
	// gitCompletionFixture gives us a bare remote repo; its own base
	// repo/worktree aren't needed here -- Manager.Run creates its own
	// worktree against a fresh base repo wired to the same remote.
	_, _, bareRemote := gitCompletionFixture(t)

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

// TestRun_ForcesAutoGitCommitForWorktreeModeOnly is the other half of
// Phase 2's fix: a worktree-mode PRD's whole purpose (a reviewable
// branch) breaks silently if the daemon's session.auto_git_commit
// default happens to be off and nothing ever gets committed. Found
// mid-implementation via a real "contains modified or untracked
// files" git worktree remove failure in the integration test above.
func TestRun_ForcesAutoGitCommitForWorktreeModeOnly(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, _ := m.CreatePRD("spec", "", "", "", "") // worktree mode
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var gotForce bool
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		gotForce = req.ForceAutoGitCommit
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gotForce {
		t.Error("ForceAutoGitCommit = false for a worktree-mode PRD, want true")
	}

	// Non-worktree PRD (explicit ProjectDir) must not force it.
	explicit := t.TempDir()
	prd2, _ := m.CreatePRD("spec", explicit, "", "", "")
	_ = m.Store().SetStories(prd2.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd2, _ = m.Store().GetPRD(prd2.ID)
	prd2.Status = PRDApproved
	_ = m.Store().SavePRD(prd2)

	gotForce = true // reset to a sentinel that proves spawn2 actually ran
	spawn2 := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		gotForce = req.ForceAutoGitCommit
		return SpawnResult{SessionID: "s2"}, nil
	}
	if err := m.Run(context.Background(), prd2.ID, spawn2, verify); err != nil {
		t.Fatalf("Run (prd2): %v", err)
	}
	if gotForce {
		t.Error("ForceAutoGitCommit = true for an explicit-ProjectDir PRD, want false")
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
