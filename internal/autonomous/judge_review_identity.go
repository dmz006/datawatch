package autonomous

// Review identity of a whole-Automaton rules review.
//
// A stored completion verdict is only worth replaying when both the code and
// the rules text are the ones it judged. stampReviewIdentity puts both on the
// invocation, so the verdict key, the stored verdict and the head comparison
// all read the same two values.

import "context"

// stampReviewIdentity sets the invocation's ReviewHead from the real head of
// the Automaton's working tree (see currentReviewHead) and its
// RulesFingerprint from the rules text the judge reads (see
// reviewRulesFingerprint). It reads only; it writes no Decision and saves
// nothing, and stamping the same unchanged state twice gives the same result.
// A nil invocation is ignored.
func (m *Manager) stampReviewIdentity(ctx context.Context, prd *PRD, inv *GuardrailInvocation) {
	if inv == nil {
		return
	}
	inv.ReviewHead = m.currentReviewHead(ctx, prd)
	inv.RulesFingerprint = m.reviewRulesFingerprint(ctx, prd)
}
