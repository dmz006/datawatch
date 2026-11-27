package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A repaired push records the landing for the exact tip that was pushed, even
// when the branch has newer work; a commit that does not hold that tip, or one
// the remote does not have, records nothing.
func TestRecordMergeLandingOfPushedTip(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, tip := r.automaton(PRDCompleted)
	commit := r.mergeLocally(p.Git.Branch)
	newer := r.commitOn(p.Git.Branch, "newer-"+p.ID)

	if _, err := r.m.RecordMergeLandingOfPushedTip(ctx, p.ID, "v1.0.1", commit, tip, time.Now()); err == nil {
		t.Fatal("recorded a landing the remote does not have")
	}
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	if _, err := r.m.RecordMergeLanding(ctx, p.ID, "v1.0.1", commit, time.Now()); err == nil {
		t.Fatal("the ordinary record must still refuse a commit that lacks the branch's head")
	}
	for name, bad := range map[string]string{"a tip the commit lacks": newer, "an option": "--all", "short": tip[:8]} {
		if _, err := r.m.RecordMergeLandingOfPushedTip(ctx, p.ID, "v1.0.1", commit, bad, time.Now()); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if got, _ := r.m.store.GetPRD(p.ID); got.IsShipped() {
		t.Fatal("a refused record wrote the shipped state")
	}
	got, err := r.m.RecordMergeLandingOfPushedTip(ctx, p.ID, "v1.0.1", commit, tip, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.MergeCommit != commit || got.Landing.State != LandingLanded {
		t.Fatalf("shipped=%q landing=%+v", got.MergeCommit, got.Landing)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if !strings.Contains(last.Note, "newer work") {
		t.Errorf("the decision does not say the branch changed: %q", last.Note)
	}
	if r.rev("refs/heads/"+p.Git.Branch) != newer {
		t.Error("the branch moved")
	}
	// The newer work is not selected again: the Automaton is shipped.
	if e := r.m.MergeEligibility(ctx, p.ID); e.Eligible {
		t.Errorf("selected again: %+v", e)
	}
}
