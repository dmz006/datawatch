// What the shadow of a hand merge asks the Manager for.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ShadowSubject is what the shadow of a hand merge needs to know about its
// Automaton.
type ShadowSubject struct {
	Name       string
	Branch     string
	BranchHead string
	Kind       string
}

// MergeShadowSubject reads the Automaton's branch, the commit it points at in
// the merge repository and its kind. false means there is no such Automaton.
func (m *Manager) MergeShadowSubject(ctx context.Context, id string) (ShadowSubject, bool) {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return ShadowSubject{}, false
	}
	var s ShadowSubject
	m.readLocked(func() {
		s = ShadowSubject{Name: p.Title, Branch: strings.TrimSpace(p.Git.Branch), Kind: EffectiveKind(p, nil)}
	})
	if repo := strings.TrimSpace(m.MergeBaseRepo()); repo != "" {
		gctx, cancel := context.WithTimeout(ctx, mergeSelectGitTimeout)
		defer cancel()
		s.BranchHead, _ = branchCommit(gctx, m.archivedUnmergedGitRunner(), repo, s.Branch)
	}
	return s, true
}

// asOfHandMerge lists the reasons that what made the work eligible was
// recorded only after the hand merge, and so proves nothing about it.
func asOfHandMerge(f mergeFacts, basis string, mergedAt time.Time) string {
	// A commit time has whole seconds.
	limit := mergedAt.Add(time.Second)
	switch {
	case basis == MergeBasisAccepted && f.acceptance != nil && f.acceptance.At.After(limit):
		return fmt.Sprintf("the commit was accepted at %s, after the hand merge", f.acceptance.At.UTC().Format(time.RFC3339))
	case basis == MergeBasisReviewPassed && f.passedAt.After(limit):
		return fmt.Sprintf("the review passed at %s, after the hand merge", f.passedAt.UTC().Format(time.RFC3339))
	}
	return ""
}

// MergeEligibilityForHandMerge answers the selection rule for a hand merge
// made at mergedAt that merged commit tip. It is the rule MergeEligibility
// applies, asked about that commit, with what the hand merge itself recorded
// (the shipped record, the landing and the delivered outcome) set aside, as is
// an operator pause, which says nothing about the work. An acceptance or a
// review pass recorded after the merge does not count. It only reads.
func (m *Manager) MergeEligibilityForHandMerge(ctx context.Context, id, tip string, mergedAt time.Time) MergeSelectionEntry {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return MergeSelectionEntry{AutomatonID: id, Reasons: []string{"there is no such Automaton"}}
	}
	var f mergeFacts
	m.readLocked(func() {
		// A shallow copy: only whole fields of it are changed.
		q := *p
		q.MergeCommit = ""
		if q.Landing != nil && q.Landing.State == LandingLanded {
			q.Landing = nil
		}
		if q.ClosedOutcome == OutcomeDelivered {
			q.ClosedOutcome = ""
		}
		f = mergeFactsOf(&q)
	})
	f.handTip = tip
	e := m.evaluateMerge(ctx, f)
	if e.Eligible {
		if late := asOfHandMerge(f, e.Basis, mergedAt); late != "" {
			e.Eligible, e.Basis, e.Reasons = false, "", []string{late}
		}
	}
	return e
}
