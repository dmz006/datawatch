// The unlanded branch list: one archived Automaton per merge case (needs a
// decision, landed on the default branch, superseded on it), each with its
// recommendation and reason.

package autonomous

import (
	"context"
	"strings"
	"testing"
)

// unlandedListFixture is a base repository on the default branch (main) with
// one initial commit, plus a Manager whose base repository is set, ready to
// receive archived records and branches.
func unlandedListFixture(t *testing.T) (string, *Manager) {
	t.Helper()
	repo := gitFixture(t)
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Config()
	cfg.WorktreeBaseRepo = repo
	m.SetConfig(cfg)
	return repo, m
}

// unlandedListRecord adds one archived Automaton record bound to a branch.
func unlandedListRecord(t *testing.T, m *Manager, branch, title string, verdicts ...GuardrailVerdict) {
	t.Helper()
	p, err := m.store.CreatePRD(title, t.TempDir(), "", "", Effort(""))
	if err != nil {
		t.Fatal(err)
	}
	p.Status = PRDArchived
	p.Git.Branch = branch
	p.Verdicts = verdicts
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

func TestListUnlandedBranchesMergeStatesAndRecommendations(t *testing.T) {
	repo, m := unlandedListFixture(t)
	git := NewMergeGitRunner()
	ctx := context.Background()

	// landed: the branch commit and the default-branch commit carry the same
	// patch (identical file, created with identical content), so the branch
	// is one commit ahead but everything is already on main by content.
	gitT(t, repo, "checkout", "-q", "-b", "automaton/landed1")
	writeFileT(t, repo, "a.txt", "same\n")
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	writeFileT(t, repo, "a.txt", "same\n")
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-q", "-m", "same change landed on main")

	// superseded: the branch changed b.txt, and main changed the same file
	// with a different patch after the fork.
	gitT(t, repo, "checkout", "-q", "-b", "automaton/super1")
	writeFileT(t, repo, "b.txt", "branch version\n")
	gitT(t, repo, "add", "b.txt")
	gitT(t, repo, "commit", "-q", "-m", "branch work")
	gitT(t, repo, "checkout", "-q", "main")
	writeFileT(t, repo, "b.txt", "main rewrote it\n")
	gitT(t, repo, "add", "b.txt")
	gitT(t, repo, "commit", "-q", "-m", "main replaced it")

	// needs_decision: branch work not on main, nothing on main touches the
	// file; the archived record carries a passing completion review.
	gitT(t, repo, "checkout", "-q", "-b", "automaton/pass1")
	writeFileT(t, repo, "c.txt", "new c\n")
	gitT(t, repo, "add", "c.txt")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")

	// needs_decision: same shape, but the newest completion review is a block.
	gitT(t, repo, "checkout", "-q", "-b", "automaton/block1")
	writeFileT(t, repo, "d.txt", "new d\n")
	gitT(t, repo, "add", "d.txt")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")

	// needs_decision: same shape, but the completion review reached no verdict.
	gitT(t, repo, "checkout", "-q", "-b", "automaton/pend1")
	writeFileT(t, repo, "e.txt", "new e\n")
	gitT(t, repo, "add", "e.txt")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")

	unlandedListRecord(t, m, "automaton/landed1", "Landed work")
	unlandedListRecord(t, m, "automaton/super1", "Superseded work")
	unlandedListRecord(t, m, "automaton/pass1", "Passing review",
		GuardrailVerdict{Guardrail: "rules-review", Outcome: "pass"})
	unlandedListRecord(t, m, "automaton/block1", "Blocked review",
		GuardrailVerdict{Guardrail: "rules-review", Outcome: "pass"},
		GuardrailVerdict{Guardrail: "rules-review", Outcome: "block"})
	unlandedListRecord(t, m, "automaton/pend1", "No verdict yet")

	got, err := ListUnlandedBranches(ctx, git, repo, m.store.ListPRDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d branches: %+v", len(got), got)
	}
	byBranch := map[string]UnlandedBranch{}
	for _, b := range got {
		byBranch[b.Branch] = b
	}
	type state struct{ merge, rec string }
	want := map[string]state{
		"automaton/landed1": {UnlandedLanded, "delete worktree"},
		"automaton/super1":  {UnlandedSuperseded, "archive branch"},
		"automaton/pass1":   {UnlandedNeedsDecision, "merge"},
		"automaton/block1":  {UnlandedNeedsDecision, "re-judge first"},
		"automaton/pend1":   {UnlandedNeedsDecision, "re-judge first"},
	}
	for br, w := range want {
		b, ok := byBranch[br]
		if !ok {
			t.Errorf("missing branch %s", br)
			continue
		}
		if b.MergeState != w.merge {
			t.Errorf("%s merge_state = %q, want %q (reason %q)", br, b.MergeState, w.merge, b.Reason)
		}
		if b.Recommendation != w.rec {
			t.Errorf("%s recommendation = %q, want %q (reason %q)", br, b.Recommendation, w.rec, b.Reason)
		}
		if b.Reason == "" {
			t.Errorf("%s has no reason", br)
		}
	}
	if !strings.Contains(byBranch["automaton/landed1"].Reason, "already on main") {
		t.Errorf("landed reason = %q", byBranch["automaton/landed1"].Reason)
	}
	if !strings.Contains(byBranch["automaton/block1"].Reason, "block") {
		t.Errorf("blocked reason = %q", byBranch["automaton/block1"].Reason)
	}
	if !strings.Contains(byBranch["automaton/pend1"].Reason, "no verdict") {
		t.Errorf("pending reason = %q", byBranch["automaton/pend1"].Reason)
	}
	// Decision-first ordering: needs_decision rows first, then superseded, then
	// landed.
	states := []string{}
	for _, b := range got {
		states = append(states, b.MergeState)
	}
	if got[0].MergeState != UnlandedNeedsDecision ||
		got[len(got)-2].MergeState != UnlandedSuperseded ||
		got[len(got)-1].MergeState != UnlandedLanded {
		t.Errorf("order = %v, want needs_decision rows first and the landed row last", states)
	}
}

func TestListUnlandedBranchesExcludesRunningAutomata(t *testing.T) {
	repo, m := unlandedListFixture(t)
	gitT(t, repo, "checkout", "-q", "-b", "automaton/running1")
	writeFileT(t, repo, "r.txt", "r\n")
	gitT(t, repo, "add", "r.txt")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	p, err := m.store.CreatePRD("Still running", t.TempDir(), "", "", Effort(""))
	if err != nil {
		t.Fatal(err)
	}
	p.Status = PRDRunning
	p.Git.Branch = "automaton/running1"
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got, err := ListUnlandedBranches(context.Background(), NewMergeGitRunner(), repo, m.store.ListPRDs())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a running Automaton must not be unlanded: %+v", got)
	}
}

