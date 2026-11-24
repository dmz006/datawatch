package autonomous

import (
	"context"
	"strings"
	"testing"
)

// unlandedFixture is a repository with one hand-ported branch: the branch adds
// only-branch.txt, main got the same work as a different commit under another
// name, so the two share no commit and no patch-id.
func unlandedFixture(t *testing.T) (repo, portCommit string) {
	t.Helper()
	repo = gitFixture(t)
	commit := func(name, content, msg string) {
		writeFileT(t, repo, name, content)
		gitT(t, repo, "add", name)
		gitT(t, repo, "commit", "-m", msg)
	}
	gitT(t, repo, "checkout", "-b", "automaton/ee570001")
	commit("only-branch.txt", "work\n", "Automaton work")
	gitT(t, repo, "checkout", "main")
	commit("hand-port.txt", "work, ported by hand\n", "Hand port of the work")
	portCommit = strings.TrimSpace(gitT(t, repo, "rev-parse", "HEAD"))
	return repo, portCommit
}

func listOne(t *testing.T, repo string, p *PRD) []UnlandedBranch {
	t.Helper()
	got, err := ListUnlandedBranches(context.Background(), NewMergeGitRunner(), repo, []*PRD{p})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A hand port shares no commit with the branch, so the content check calls it
// needs_decision. The shipped record names the hand-port commit on main, which
// proves delivery: the list must not report it as unlanded.
func TestUnlandedList_ShippedRecordOnMainMarksHandPortLanded(t *testing.T) {
	repo, port := unlandedFixture(t)
	p := &PRD{ID: "ee570001", Title: "Hand ported", Status: PRDArchived,
		MergedInRelease: "9.183.0", MergeCommit: port}
	got := listOne(t, repo, p)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].Recommendation != UnlandedLanded {
		t.Fatalf("recommendation = %q, want %q (reason %q)", got[0].Recommendation, UnlandedLanded, got[0].Reason)
	}
	if !strings.Contains(got[0].Reason, "9.183.0") || !strings.Contains(got[0].Reason, port[:7]) {
		t.Errorf("reason does not name the shipped record: %q", got[0].Reason)
	}
}

// A shipped record is only trusted when its commit is on the default branch.
func TestUnlandedList_ShippedRecordNotOnMainStaysNeedsDecision(t *testing.T) {
	repo, _ := unlandedFixture(t)
	gitT(t, repo, "checkout", "-b", "elsewhere", "main")
	writeFileT(t, repo, "elsewhere.txt", "e\n")
	gitT(t, repo, "add", "elsewhere.txt")
	gitT(t, repo, "commit", "-m", "Commit that never reached main")
	off := strings.TrimSpace(gitT(t, repo, "rev-parse", "HEAD"))
	gitT(t, repo, "checkout", "main")
	p := &PRD{ID: "ee570001", Title: "Hand ported", Status: PRDArchived,
		MergedInRelease: "9.183.0", MergeCommit: off}
	got := listOne(t, repo, p)
	if len(got) != 1 || got[0].Recommendation != UnlandedNeedsDecision {
		t.Fatalf("want one needs_decision entry, got %+v", got)
	}
}

// With no shipped record, a hand port is still matched by content: every file
// the branch changed has the same content on the default branch.
func TestUnlandedList_SameContentOnMainMarksLanded(t *testing.T) {
	repo := gitFixture(t)
	gitT(t, repo, "checkout", "-b", "automaton/c0a70001")
	writeFileT(t, repo, "f.txt", "same\n")
	gitT(t, repo, "add", "f.txt")
	gitT(t, repo, "commit", "-m", "Automaton work")
	gitT(t, repo, "checkout", "main")
	// The port lands the same file together with another one, so the patch-id
	// differs and git cherry still says the commit is not on main.
	writeFileT(t, repo, "f.txt", "same\n")
	writeFileT(t, repo, "g.txt", "more\n")
	gitT(t, repo, "add", "f.txt", "g.txt")
	gitT(t, repo, "commit", "-m", "Port of the work with extras")
	got := listOne(t, repo, &PRD{ID: "c0a70001", Title: "Same content", Status: PRDArchived})
	if len(got) != 1 || got[0].Recommendation != UnlandedLanded {
		t.Fatalf("want one landed entry, got %+v", got)
	}
	if !strings.Contains(got[0].Reason, "content") {
		t.Errorf("reason does not say matched by content: %q", got[0].Reason)
	}
}

// Different content on main is not a match: the branch stays needs_decision.
func TestUnlandedList_DifferentContentStaysNeedsDecision(t *testing.T) {
	repo, _ := unlandedFixture(t)
	got := listOne(t, repo, &PRD{ID: "ee570001", Title: "No record", Status: PRDArchived})
	if len(got) != 1 || got[0].Recommendation != UnlandedNeedsDecision {
		t.Fatalf("want one needs_decision entry, got %+v", got)
	}
}

// A recorded landing decision (drop or keep) leaves the list; a pending
// landing record does not.
func TestUnlandedList_LandingDecisionLeavesList(t *testing.T) {
	repo, _ := unlandedFixture(t)
	for _, tc := range []struct {
		state LandingState
		want  int
	}{
		{LandingDropped, 0}, {LandingKept, 0}, {LandingPending, 1},
	} {
		p := &PRD{ID: "ee570001", Title: "Decided", Status: PRDArchived,
			Landing: &Landing{State: tc.state, Actor: "operator", Reason: "r"}}
		if got := listOne(t, repo, p); len(got) != tc.want {
			t.Errorf("landing %s: got %d entries, want %d: %+v", tc.state, len(got), tc.want, got)
		}
	}
}

// The API value the daemon wires must carry the landing decisions, otherwise
// the REST route answers 501 "not supported by this daemon".
func TestAPIForwardsLandingDecisions(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.store.WithLock(func() { prd.Git.Branch = "automaton/" + prd.ID })
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(m)
	decider, ok := any(api).(interface {
		KeepLanding(id, actor, reason string) (*PRD, error)
		DropLanding(id, actor, reason string) (*PRD, error)
	})
	if !ok {
		t.Fatal("*API does not implement KeepLanding and DropLanding")
	}
	out, err := decider.DropLanding(prd.ID, "operator", "hand ported")
	if err != nil {
		t.Fatal(err)
	}
	if out.Landing == nil || out.Landing.State != LandingDropped {
		t.Fatalf("landing after drop = %+v", out.Landing)
	}
}
