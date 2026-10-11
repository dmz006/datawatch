// BL407 Phase 3 — cluster-mode git completion (handleClusterCompletion)
// tests. Unlike worktree mode, there is no local push here to exercise
// against a real bare remote — the worker already did that itself
// (agents.PushOnCompletion, tested in internal/agents); these tests
// only cover the parent-side "open a PR from the reported branch"
// half, plus the mode-separation guards on both completion paths.
package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

func TestHandleClusterCompletion_NoOpWhenNotClusterMode(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", Git: GitWorkflow{Branch: "automaton/abc12345", URL: "https://github.com/x/y", AutoPR: true}}
	m.handleClusterCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when ClusterProfile is empty")
	}
}

func TestHandleClusterCompletion_NoOpWhenBranchEmpty(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", ClusterProfile: "k8s-default", Git: GitWorkflow{URL: "https://github.com/x/y", AutoPR: true}}
	m.handleClusterCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when Git.Branch is empty — the worker never reported one")
	}
}

func TestHandleClusterCompletion_NoOpWhenAutoPRFalse(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", ClusterProfile: "k8s-default", Git: GitWorkflow{Branch: "automaton/abc12345", URL: "https://github.com/x/y"}}
	m.handleClusterCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when Git.AutoPR is false")
	}
}

func TestHandleClusterCompletion_NoURLRecordsFailure(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.ClusterProfile = "k8s-default"
	prd.Git.Branch = "automaton/" + prd.ID
	prd.Git.AutoPR = true
	// Git.URL deliberately left empty: the dispatching project profile
	// had no Git.URL to resolve (see autonomousVerify's capture logic).
	_ = m.Store().SavePRD(prd)

	m.handleClusterCompletion(prd)

	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when there's no URL to open a PR against")
	}
	updated, _ := m.Store().GetPRD(prd.ID)
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_failed Decision to be recorded")
	}
}

func TestHandleClusterCompletion_OpensPRFromReportedBranch(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{url: "https://github.com/x/y/pull/7"}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.ClusterProfile = "k8s-default"
	prd.Title = "Cluster task"
	prd.Git.Branch = "automaton/" + prd.ID
	prd.Git.URL = "https://github.com/x/y"
	prd.Git.AutoPR = true
	_ = m.Store().SavePRD(prd)

	m.handleClusterCompletion(prd)

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 OpenPR call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	if got.HeadBranch != prd.Git.Branch {
		t.Errorf("HeadBranch = %q, want %q", got.HeadBranch, prd.Git.Branch)
	}
	if got.Repo != "x/y" {
		t.Errorf("Repo = %q, want x/y", got.Repo)
	}

	updated, _ := m.Store().GetPRD(prd.ID)
	if updated.Git.PRURL != "https://github.com/x/y/pull/7" {
		t.Errorf("Git.PRURL = %q, want the fake's URL", updated.Git.PRURL)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_opened" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_opened Decision to be recorded")
	}
}

func TestHandleClusterCompletion_OpenPRFailureRecordsDecision(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{err: context.DeadlineExceeded}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.ClusterProfile = "k8s-default"
	prd.Git.Branch = "automaton/" + prd.ID
	prd.Git.URL = "https://github.com/x/y"
	prd.Git.AutoPR = true
	_ = m.Store().SavePRD(prd)

	m.handleClusterCompletion(prd)

	if len(fake.calls) != 1 {
		t.Fatalf("expected OpenPR to be attempted once, got %d", len(fake.calls))
	}
	updated, _ := m.Store().GetPRD(prd.ID)
	if updated.Git.PRURL != "" {
		t.Errorf("Git.PRURL must stay empty on OpenPR failure, got %q", updated.Git.PRURL)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_failed Decision to be recorded")
	}
}

// handleWorktreeCompletion must defer entirely to handleClusterCompletion
// for cluster-dispatched PRDs — it has no ProjectDir to push from (one
// was never created; EnsureWorktree's own gating excludes ClusterProfile
// PRDs) and must not attempt to run git against an empty directory.
func TestHandleWorktreeCompletion_NoOpWhenClusterMode(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", ClusterProfile: "k8s-default", Git: GitWorkflow{Branch: "automaton/abc12345", URL: "https://github.com/x/y", AutoPR: true}}
	m.handleWorktreeCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("handleWorktreeCompletion must not call OpenPR for a cluster-dispatched PRD")
	}
}
