package autonomous

import (
	"context"
	"testing"
)

func TestListUnlandedBranches(t *testing.T) {
	repo := gitFixture(t)
	commitFile := func(name, content, msg string) {
		writeFileT(t, repo, name, content)
		gitT(t, repo, "add", name)
		gitT(t, repo, "commit", "-m", msg)
	}
	// rewritten merge: same change lands on main with a different subject.
	gitT(t, repo, "checkout", "-b", "automaton/aaaa0001")
	commitFile("landed.txt", "landed\n", "Automaton work A")
	gitT(t, repo, "checkout", "main")
	gitT(t, repo, "cherry-pick", "automaton/aaaa0001")
	gitT(t, repo, "commit", "--amend", "-m", "Renumbered merge of work A")
	// truly unlanded: touches a file main never changes.
	gitT(t, repo, "checkout", "-b", "automaton/bbbb0002", "main")
	commitFile("only-here.txt", "x\n", "Automaton work B")
	// superseded: branch edits a file that main later rewrote differently.
	gitT(t, repo, "checkout", "-b", "automaton/cccc0003", "main")
	commitFile("shared.txt", "branch version\n", "Automaton work C")
	gitT(t, repo, "checkout", "main")
	commitFile("shared.txt", "newer main version\n", "Rework shared file")
	// live: running Automaton, unlanded commits, must be excluded.
	gitT(t, repo, "checkout", "-b", "automaton/dddd0004", "main")
	commitFile("live.txt", "l\n", "Automaton work D")
	gitT(t, repo, "checkout", "main")

	prds := []*PRD{
		{ID: "aaaa0001", Title: "Landed work", Status: PRDArchived},
		{ID: "bbbb0002", Spec: "Unlanded work\nmore", Status: PRDArchived},
		{ID: "cccc0003", Title: "Superseded work", Status: PRDArchived},
		{ID: "dddd0004", Title: "Live work", Status: PRDRunning},
	}
	got, err := ListUnlandedBranches(context.Background(), NewMergeGitRunner(), repo, prds)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id, state, rec, title string
		commits               int
	}{
		// No PRD in this fixture carries a completion-verdict, so the
		// needs_decision row re-judges before merging; a passing review gets the
		// "merge" recommendation (TestListUnlandedBranchesMergeStates…).
		{"bbbb0002", UnlandedNeedsDecision, "re-judge first", "Unlanded work", 1},
		{"cccc0003", UnlandedSuperseded, "archive branch", "Superseded work", 1},
		{"aaaa0001", UnlandedLanded, "delete worktree", "Landed work", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.AutomatonID != w.id || g.MergeState != w.state || g.Recommendation != w.rec || g.Title != w.title || g.Commits != w.commits {
			t.Errorf("entry %d = %+v, want %+v", i, g, w)
		}
		if g.Reason == "" || g.LastActivity.IsZero() {
			t.Errorf("entry %d missing reason or activity: %+v", i, g)
		}
	}
}
