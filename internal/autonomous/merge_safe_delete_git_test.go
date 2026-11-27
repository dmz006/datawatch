package autonomous

// The safe-to-delete report against real git: its listing prints each
// branch's counts as "<ahead> <behind>". Throwaway repositories only.

import (
	"context"
	"testing"
)

// sdRealCommit adds one commit to branch, which is created from trunk when it
// does not exist, and leaves the repository on trunk.
func sdRealCommit(t *testing.T, repo, branch, file string) {
	t.Helper()
	if !branchExists(t, repo, branch) {
		gitCmd(t, repo, "branch", branch, "trunk")
	}
	gitCmd(t, repo, "checkout", "-q", branch)
	keepWrite(t, repo, file, file+"\n")
	gitCmd(t, repo, "add", file)
	gitCmd(t, repo, "commit", "-q", "-m", "add "+file)
	gitCmd(t, repo, "checkout", "-q", "trunk")
}

func sdRealReport(t *testing.T, repo string) (safe map[string]SafeDeleteEntry, held map[string]SafeDeleteHeld) {
	t.Helper()
	rep, err := SafeToDeleteReport(context.Background(), NewMergeGitRunner(), repo, "trunk", SafeDeleteOptions{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	safe, held = map[string]SafeDeleteEntry{}, map[string]SafeDeleteHeld{}
	for _, e := range rep.Safe {
		safe[e.Branch] = e
	}
	for _, h := range rep.Held {
		held[h.Branch] = h
	}
	return safe, held
}

func TestSafeToDeleteRealGitHoldsAnUnmergedBranchOnAnUnmovedBase(t *testing.T) {
	repo, _ := fixtureRepo(t)
	sdRealCommit(t, repo, "automaton/unmerged", "unmerged.txt")

	safe, held := sdRealReport(t, repo)

	if e, ok := safe["automaton/unmerged"]; ok {
		t.Fatalf("an unmerged branch is listed as safe to delete: %+v", e)
	}
	if h, ok := held["automaton/unmerged"]; !ok || h.State != MergeStatePending {
		t.Fatalf("held = %+v (listed %v), want pending", h, ok)
	}
}

func TestSafeToDeleteRealGitBranchOnlyBehindHasNothingAhead(t *testing.T) {
	repo, _ := fixtureRepo(t)
	gitCmd(t, repo, "branch", "automaton/behind", "trunk")
	for i := 0; i < 3; i++ {
		gitCmd(t, repo, "commit", "-q", "--allow-empty", "-m", "base moves on "+string(rune('a'+i)))
	}

	safe, held := sdRealReport(t, repo)

	e, ok := safe["automaton/behind"]
	if !ok || e.Commits != 0 || e.Reason != safeReasonEmpty("trunk") {
		t.Fatalf("safe = %+v (listed %v), held = %+v", e, ok, held["automaton/behind"])
	}
}

func TestSafeToDeleteRealGitCountsTheCommitsAhead(t *testing.T) {
	repo, _ := fixtureRepo(t)
	sdRealCommit(t, repo, "automaton/picked", "one.txt")
	sdRealCommit(t, repo, "automaton/picked", "two.txt")
	for i := 0; i < 3; i++ {
		gitCmd(t, repo, "commit", "-q", "--allow-empty", "-m", "base moves on "+string(rune('a'+i)))
	}
	gitCmd(t, repo, "cherry-pick", "automaton/picked~1", "automaton/picked")

	safe, held := sdRealReport(t, repo)

	e, ok := safe["automaton/picked"]
	if !ok || e.Commits != 2 || e.Reason != safeReasonMerged("trunk") {
		t.Fatalf("safe = %+v (listed %v), held = %+v", e, ok, held["automaton/picked"])
	}
}
