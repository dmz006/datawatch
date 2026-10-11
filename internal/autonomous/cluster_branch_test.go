// BL407 Phase 4 — Manager.Run assigns a dedicated branch name to a
// cluster-dispatched PRD (the cluster-mode counterpart to Phase 1's
// EnsureWorktree), and threads it through SpawnRequest.Branch so
// SpawnFn can forward it to agents.SpawnRequest.Branch.
package autonomous

import (
	"context"
	"testing"
)

func newApprovedClusterPRD(t *testing.T, m *Manager, clusterProfile string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.ClusterProfile = clusterProfile
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	return prd
}

func TestRun_AssignsClusterBranchWhenClusterProfileSet(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd := newApprovedClusterPRD(t, m, "k8s-default")

	var gotBranch, gotClusterProfile string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		gotBranch = req.Branch
		gotClusterProfile = req.ClusterProfile
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Git.Branch == "" {
		t.Fatal("Git.Branch was not assigned for a cluster-dispatched PRD")
	}
	if gotBranch != got.Git.Branch {
		t.Errorf("SpawnRequest.Branch=%q, want the persisted Git.Branch %q", gotBranch, got.Git.Branch)
	}
	if gotClusterProfile != "k8s-default" {
		t.Errorf("SpawnRequest.ClusterProfile=%q, want k8s-default", gotClusterProfile)
	}
	want := branchNameFor(got)
	if got.Git.Branch != want {
		t.Errorf("Git.Branch=%q, want branchNameFor's own result %q", got.Git.Branch, want)
	}
}

// A PRD resumed across a daemon restart (Status already PRDRunning,
// Git.Branch already persisted from a prior Run) must not regenerate
// a second branch name — same idempotency guarantee Phase 1's
// EnsureWorktree gives worktree mode.
func TestRun_ClusterBranchAssignmentIsIdempotent(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd := newApprovedClusterPRD(t, m, "k8s-default")

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first, _ := m.Store().GetPRD(prd.ID)
	firstBranch := first.Git.Branch

	// Simulate a resume: the single task already completed and the PRD
	// rolled up to PRDCompleted, which Run() itself would reject as
	// "not runnable" — force it back to PRDRunning the way a daemon
	// restart's boot-resume path would find it mid-flight, so this
	// test exercises only the branch-assignment idempotency, not a
	// second real task dispatch.
	first.Status = PRDRunning
	if err := m.Store().SavePRD(first); err != nil {
		t.Fatalf("force status to running: %v", err)
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second, _ := m.Store().GetPRD(prd.ID)
	if second.Git.Branch != firstBranch {
		t.Errorf("Git.Branch changed across resume: first=%q second=%q", firstBranch, second.Git.Branch)
	}
}
