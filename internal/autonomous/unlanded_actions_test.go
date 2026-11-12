package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// actionsGit wraps a runner, records calls and can fail a chosen subcommand.
type actionsGit struct {
	inner MergeGitRunner
	calls []string
	fail  string
}

func (r *actionsGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	if r.fail != "" && len(args) > 0 && strings.Join(args[:min(len(args), 2)], " ") == r.fail {
		return "", errors.New("forced failure")
	}
	return r.inner.Run(ctx, dir, args...)
}

func unlandedActionsRepo(t *testing.T) string {
	repo := gitFixture(t)
	gitT(t, repo, "checkout", "-b", "automaton/eeee0005")
	writeFileT(t, repo, "x.txt", "x\n")
	gitT(t, repo, "add", "x.txt")
	gitT(t, repo, "commit", "-m", "work")
	gitT(t, repo, "checkout", "main")
	return repo
}

func actionsBranchExists(t *testing.T, repo, b string) bool {
	_, err := NewMergeGitRunner().Run(context.Background(), repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+b)
	return err == nil
}

func TestDeleteWorktreeRefusals(t *testing.T) {
	repo := unlandedActionsRepo(t)
	base := t.TempDir()
	git := &actionsGit{inner: NewMergeGitRunner()}
	ctx := context.Background()

	_, err := DeleteWorktree(ctx, git, DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005"}, Confirm: true, BaseRepo: repo, WorktreeBase: base})
	if err == nil || !strings.Contains(err.Error(), "not classified") {
		t.Fatalf("unclassified: err = %v", err)
	}
	_, err = DeleteWorktree(ctx, git, DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005", Recommendation: UnlandedNeedsDecision}, BaseRepo: repo, WorktreeBase: base})
	if err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("no confirm: err = %v", err)
	}
	if len(git.calls) != 0 {
		t.Errorf("refusal ran git: %v", git.calls)
	}
	if !actionsBranchExists(t, repo, "automaton/eeee0005") {
		t.Error("branch removed on refusal")
	}
	if _, err := os.Stat(filepath.Join(base, UnlandedBackupsDir)); err == nil {
		t.Error("backup folder created on refusal")
	}
}

func TestDeleteWorktreeBackupFirst(t *testing.T) {
	repo := unlandedActionsRepo(t)
	base := t.TempDir()
	git := &actionsGit{inner: NewMergeGitRunner()}
	res, err := DeleteWorktree(context.Background(), git, DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005", Recommendation: UnlandedNeedsDecision}, Confirm: true, BaseRepo: repo, WorktreeBase: base})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.BundlePath, filepath.Join(base, UnlandedBackupsDir)) {
		t.Errorf("bundle path %s", res.BundlePath)
	}
	if _, err := os.Stat(res.BundlePath); err != nil {
		t.Fatalf("bundle missing: %v", err)
	}
	if actionsBranchExists(t, repo, "automaton/eeee0005") {
		t.Error("branch still exists")
	}
	bundleIdx, deleteIdx := -1, -1
	for i, c := range git.calls {
		if strings.HasPrefix(c, "bundle create") {
			bundleIdx = i
		}
		if strings.HasPrefix(c, "branch -D") || strings.HasPrefix(c, "worktree remove") {
			if deleteIdx < 0 {
				deleteIdx = i
			}
		}
	}
	if bundleIdx < 0 || deleteIdx < 0 || bundleIdx > deleteIdx {
		t.Errorf("backup must precede removal: %v", git.calls)
	}
	// The bundle restores the branch.
	gitT(t, repo, "fetch", res.BundlePath, "refs/heads/automaton/eeee0005:refs/heads/restored")
	if !actionsBranchExists(t, repo, "restored") {
		t.Error("bundle did not restore the branch")
	}
}

func TestDeleteWorktreeAbortsWhenBundleFails(t *testing.T) {
	repo := unlandedActionsRepo(t)
	base := t.TempDir()
	git := &actionsGit{inner: NewMergeGitRunner(), fail: "bundle create"}
	_, err := DeleteWorktree(context.Background(), git, DeleteWorktreeOptions{
		Branch: UnlandedBranch{Branch: "automaton/eeee0005", Recommendation: UnlandedLanded}, Confirm: true, BaseRepo: repo, WorktreeBase: base})
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("err = %v", err)
	}
	if !actionsBranchExists(t, repo, "automaton/eeee0005") {
		t.Error("branch removed although backup failed")
	}
	for _, c := range git.calls {
		if strings.HasPrefix(c, "branch -D") || strings.HasPrefix(c, "worktree remove") {
			t.Errorf("removal ran after failed backup: %s", c)
		}
	}
}

func TestKeepAndArchive(t *testing.T) {
	repo := unlandedActionsRepo(t)
	state := t.TempDir()
	if UnlandedKept(state, "automaton/eeee0005") {
		t.Fatal("kept before Keep")
	}
	if err := Keep(state, "automaton/eeee0005", "needed later"); err != nil {
		t.Fatal(err)
	}
	if err := Keep(state, "automaton/eeee0005", "still needed"); err != nil {
		t.Fatal(err)
	}
	if !UnlandedKept(state, "automaton/eeee0005") || UnlandedKept(state, "automaton/other") {
		t.Error("keep lookup wrong")
	}
	ref, err := ArchiveBranch(context.Background(), NewMergeGitRunner(), repo, "automaton/eeee0005")
	if err != nil {
		t.Fatal(err)
	}
	if ref != UnlandedArchiveRefPrefix+"automaton/eeee0005" {
		t.Errorf("ref = %s", ref)
	}
	if actionsBranchExists(t, repo, "automaton/eeee0005") {
		t.Error("branch name still present")
	}
	gitT(t, repo, "rev-parse", "--verify", ref)
}
