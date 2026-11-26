package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The shadow asks the selection rule as it stood at the hand merge: the
// shipped record the hand merge wrote does not count against it, and nothing
// recorded after the merge counts for it.
func TestMergeEligibilityForHandMerge(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	p, tip := r.automaton(PRDCompleted)
	p.Kind = "bug"
	p.MergeAcceptance = meAccept(tip)
	p.MergeAcceptance.At = time.Now().Add(-time.Hour)
	r.save(p)
	mergedAt := time.Now().Add(-time.Minute)

	// What a hand merge and its record leave behind.
	commit := r.mergeLocally(p.Git.Branch)
	gitRun(t, r.repo, "push", "-q", "origin", "main")
	if _, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", mergedAt, commit, ShippedSourceMergeStep); err != nil {
		t.Fatal(err)
	}
	r.m.SetPauseGate(mePause{})
	if now := r.m.MergeEligibility(ctx, p.ID); now.Eligible {
		t.Fatal("fixture: the rule as it stands now must exclude shipped work")
	}
	got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, tip, mergedAt)
	if !got.Eligible || got.Basis != MergeBasisAccepted || got.Tip != tip {
		t.Errorf("as of the hand merge: %+v", got)
	}
	// The branch may have gone or moved since.
	newer := r.commitOn(p.Git.Branch, "later-"+p.ID)
	if got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, tip, mergedAt); !got.Eligible {
		t.Errorf("after the branch moved: %+v", got)
	}
	// What was merged is not what was accepted.
	if got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, newer, mergedAt); got.Eligible {
		t.Errorf("an acceptance covered a later commit: %+v", got)
	}
	// An acceptance recorded after the merge proves nothing about it.
	if got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, tip, time.Now().Add(-2*time.Hour)); got.Eligible ||
		!strings.Contains(strings.Join(got.Reasons, " "), "after the hand merge") {
		t.Errorf("a late acceptance counted: %+v", got)
	}
	// The record itself is not changed by asking.
	after, _ := r.m.store.GetPRD(p.ID)
	if !after.IsShipped() || after.Landing == nil {
		t.Errorf("asking changed the record: %+v", after.Landing)
	}
	s, ok := r.m.MergeShadowSubject(ctx, p.ID)
	if !ok || s.Branch != p.Git.Branch || s.BranchHead != newer || s.Kind != "bug" {
		t.Errorf("subject %+v %v", s, ok)
	}
}

func TestMergeEligibilityForHandMergeStillBarsFailedAndBlockedWork(t *testing.T) {
	r := newMERig(t)
	ctx := context.Background()
	for _, status := range []PRDStatus{PRDFailed, PRDBlocked, PRDCancelled} {
		p, tip := r.automaton(status)
		p.MergeAcceptance = meAccept(tip)
		p.MergeAcceptance.At = time.Now().Add(-time.Hour)
		r.save(p)
		if got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, tip, time.Now()); got.Eligible {
			t.Errorf("%s work was eligible", status)
		}
	}
	// A review that passed only after the merge does not count either.
	p, tip := r.automaton(PRDCompleted)
	p.Verdicts = []GuardrailVerdict{meReview("pass", tip)}
	r.save(p)
	if got := r.m.MergeEligibilityForHandMerge(ctx, p.ID, tip, time.Now().Add(-time.Hour)); got.Eligible {
		t.Errorf("a later review counted: %+v", got)
	}
}
