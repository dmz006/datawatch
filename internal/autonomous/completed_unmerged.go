// Completed-but-unmerged listing. The completion landing gate refuses a new
// completion whose branch never landed, but records completed before the gate
// (or completed through a path it does not cover) can still read completed
// while their branch sits outside the default branch. This file lists those
// Automata so the supervisor watch can raise one finding each. It only reads:
// git state comes through CompletionLandingGit (so tests fake it) and nothing
// here merges, reverts or edits a record.
package autonomous

import (
	"context"
	"sort"
	"strings"
)

// CompletedUnmerged is one completed Automaton whose branch is not merged
// into the default branch and which carries no no-code reason.
type CompletedUnmerged struct {
	ID     string
	Branch string
	// Detail says why the branch is not known to be merged.
	Detail string
}

// ListCompletedUnmerged returns, sorted by id, every completed Automaton in
// prds whose work branch is not merged into the default branch of repo and
// whose record has no recorded evidence that nothing needs landing (no-code
// reason, merge commit, landing record, default-branch work). judge returns
// an Automaton's latest judge outcome; a block or fail verdict is skipped,
// since that work was never accepted as complete. A nil judge reads every
// Automaton as accepted. A nil git cannot prove a merge, so it lists
// nothing rather than guessing.
func ListCompletedUnmerged(ctx context.Context, git CompletionLandingGit, repo string, prds []*PRD, judge func(*PRD) string) []CompletedUnmerged {
	if git == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var out []CompletedUnmerged
	for _, p := range prds {
		if p == nil || p.Status != PRDCompleted {
			continue
		}
		if recordedLandingEvidence(p) != "" {
			continue
		}
		if judge != nil {
			switch strings.ToLower(strings.TrimSpace(judge(p))) {
			case "block", "fail":
				continue
			}
		}
		branch := strings.TrimSpace(p.Git.Branch)
		dir := repo
		if dir == "" {
			dir = strings.TrimSpace(p.ProjectDir)
		}
		merged, detail := git.BranchMerged(ctx, dir, branch)
		if merged {
			continue
		}
		if detail == "" {
			detail = "the branch is not an ancestor of the default branch"
		}
		out = append(out, CompletedUnmerged{ID: p.ID, Branch: branch, Detail: detail})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// supervisorWatchCompletedUnmergedFindings builds one completed_unmerged
// finding per listed Automaton.
func (m *Manager) supervisorWatchCompletedUnmergedFindings(prds []*PRD) []SupervisorWatchFinding {
	_, git, repo := m.completionLandingSettings()
	var out []SupervisorWatchFinding
	for _, u := range ListCompletedUnmerged(context.Background(), git, repo, prds, nil) {
		out = append(out, supervisorWatchWorkFinding(SupervisorWatchCompletedUnmerged, u.ID))
	}
	return out
}
