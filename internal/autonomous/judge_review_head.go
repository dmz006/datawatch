package autonomous

// Current review head: the commit the completion judge actually reads.
//
// The completion review used to anchor only on the latest recorded task
// attempt commit. A fix that lands any other way (an operator commit, a
// tracker entry added by hand) leaves that anchor unchanged, so a stored
// verdict could be replayed against a tree it never saw. The real head of
// the Automaton's own working tree is the commit the judge reads, so it is
// the right anchor. The recorded attempt commit stays as the fallback when
// the head cannot be read.

import (
	"context"
	"strings"
)

// currentReviewHead returns the full commit id of the HEAD of the
// Automaton's own working tree (prd.ProjectDir), read through the injectable
// git runner. When prd is nil, ProjectDir is empty, the runner fails or it
// prints nothing, it falls back to the latest recorded attempt commit, which
// is "" when none exists. It is read-only: it never writes and never changes
// prd.
func (m *Manager) currentReviewHead(ctx context.Context, prd *PRD) string {
	if prd == nil {
		return ""
	}
	if prd.ProjectDir != "" {
		out, err := m.baseCommitRunner()(ctx, prd.ProjectDir, "rev-parse", "HEAD")
		if err == nil {
			if sha := strings.TrimSpace(out); sha != "" {
				return sha
			}
		}
	}
	return judgeVerdictCommitSHA(prd, GuardrailInvocation{})
}
