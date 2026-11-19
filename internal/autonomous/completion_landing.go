// Completion landing gate. An Automaton is only reported completed when its
// work has landed: the branch is merged into the default branch, a merge or
// shipped record names the commit, or the record carries an explicit no-code
// reason. Every path that sets the completed status (the completion rollup,
// the judge re-run that accepts a warning, the post-ship hook) asks this gate
// first. A refused change leaves the status as it was and records the reason
// on the Automaton and in its decision log. The archive eligibility check
// reads the same recorded evidence. The gate only reads git (through an
// interface tests can fake); it never merges, reverts or edits git state.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// DecisionCompletionRefused is the Decision kind written when the gate
// refuses a change to completed.
const DecisionCompletionRefused = "completion_refused"

// Completion paths named in the refusal record.
const (
	CompletionPathRollup   = "rollup"
	CompletionPathRejudge  = "judge_warn_accepted"
	CompletionPathPostShip = "post_ship"
	CompletionPathArchive  = "archive"
)

// CompletionLandingGit answers whether a branch is merged into the default
// branch of a repository. It is read-only.
type CompletionLandingGit interface {
	// BranchMerged reports whether branch is merged into the default branch of
	// repo; detail says why when it is not (or could not be decided).
	BranchMerged(ctx context.Context, repo, branch string) (merged bool, detail string)
}

// completionLandingGitDefault builds the checker a manager uses when none was
// set. It is a variable so the package's tests, which finish fake worktree
// Automata that have no real repository, can replace it.
var completionLandingGitDefault = NewCompletionLandingGit

// gitCompletionLanding is the production CompletionLandingGit. It reuses the
// read-only merge proof (ancestry, then patch equivalence for squashes).
type gitCompletionLanding struct{ git MergeGitRunner }

// NewCompletionLandingGit returns the production ancestry checker.
func NewCompletionLandingGit() CompletionLandingGit {
	return gitCompletionLanding{git: NewMergeGitRunner()}
}

func (g gitCompletionLanding) BranchMerged(ctx context.Context, repo, branch string) (bool, string) {
	p := MergedMemberProof(ctx, g.git, repo, branch)
	return p.Merged, p.Reason
}

// CompletionLandingDecision is the gate's answer.
type CompletionLandingDecision struct {
	Allowed bool
	// Reason is the plain sentence stored when the change is refused, or the
	// evidence when it is allowed.
	Reason string
}

// recordedLandingEvidence returns the evidence a record already carries that
// its work landed or needs no landing, or "" when there is none.
func recordedLandingEvidence(p *PRD) string {
	switch {
	case strings.TrimSpace(p.NoCodeReason) != "":
		return "no-code reason recorded: " + strings.TrimSpace(p.NoCodeReason)
	case strings.TrimSpace(p.MergeCommit) != "":
		return "shipped record names merge commit " + p.MergeCommit
	case p.Landing != nil && p.Landing.State == LandingLanded:
		return "landing record says the branch landed"
	case p.Landing != nil && (p.Landing.State == LandingKept || p.Landing.State == LandingDropped):
		return "operator recorded the branch as " + string(p.Landing.State)
	case strings.TrimSpace(p.Git.Branch) == "":
		return "no work branch: nothing to land"
	case onDefaultBranch(p):
		return "the work is on the default branch itself: nothing to land"
	}
	return ""
}

// onDefaultBranch reports whether the Automaton works directly on the default
// branch (main, master or its own recorded base branch), so no separate
// branch needs merging.
func onDefaultBranch(p *PRD) bool {
	b := strings.TrimSpace(p.Git.Branch)
	return b == "main" || b == "master" || (b != "" && b == strings.TrimSpace(p.Git.BaseBranch))
}

// EvaluateCompletionLanding decides whether p may become completed. judge is
// the latest judge outcome ("pass", "warn", "block" or "fail"; "" for none).
// A failed judge never completes. A pass or a warn completes only with
// landing evidence: recorded evidence first, then (when git is not nil) the
// ancestry check of the branch in repo. extraCommit is a merge commit the
// caller already knows (the post-ship hook). A disabled gate allows all.
func EvaluateCompletionLanding(ctx context.Context, enabled bool, git CompletionLandingGit, repo string, p *PRD, judge, extraCommit string) CompletionLandingDecision {
	if !enabled {
		return CompletionLandingDecision{Allowed: true, Reason: "completion landing gate is off"}
	}
	if p == nil {
		return CompletionLandingDecision{Reason: "no Automaton record"}
	}
	switch strings.ToLower(strings.TrimSpace(judge)) {
	case "block", "fail":
		return CompletionLandingDecision{Reason: "the judge verdict is " + strings.TrimSpace(judge) + ", so the work is not accepted as complete"}
	}
	if strings.TrimSpace(extraCommit) != "" {
		return CompletionLandingDecision{Allowed: true, Reason: "merge commit " + extraCommit + " supplied"}
	}
	if ev := recordedLandingEvidence(p); ev != "" {
		return CompletionLandingDecision{Allowed: true, Reason: ev}
	}
	branch := strings.TrimSpace(p.Git.Branch)
	detail := "no ancestry check is available"
	if git != nil {
		merged, d := git.BranchMerged(ctx, repo, branch)
		if merged {
			return CompletionLandingDecision{Allowed: true, Reason: "branch " + branch + " is merged into the default branch"}
		}
		if d != "" {
			detail = d
		} else {
			detail = "the branch is not an ancestor of the default branch"
		}
	}
	via := ""
	if j := strings.ToLower(strings.TrimSpace(judge)); j == "warn" {
		via = " (the judge returned a warning, which does not replace a merge)"
	}
	return CompletionLandingDecision{Reason: fmt.Sprintf(
		"completion refused: branch %s is not merged into the default branch%s and the record has no no-code reason: %s. Merge the branch, or record a no-code reason.",
		branch, via, detail)}
}

// SetCompletionLandingConfig sets the autonomous.completion_landing settings.
func (m *Manager) SetCompletionLandingConfig(c config.CompletionLandingConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completionLanding = c
}

// SetCompletionLandingGit replaces the git checker; nil restores production.
func (m *Manager) SetCompletionLandingGit(g CompletionLandingGit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completionLandingGit = g
}

func (m *Manager) completionLandingSettings() (bool, CompletionLandingGit, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.completionLandingGit
	if g == nil {
		g = completionLandingGitDefault()
	}
	return m.completionLanding.EffectiveEnabled(), g, strings.TrimSpace(m.cfg.WorktreeBaseRepo)
}

// latestJudgeOutcome returns "warn" when the newest guardrail verdict is a
// warning and "pass" otherwise. A rollup only reaches the gate after the
// judges let the work through, and an older block (already fixed by corrective
// work) stays on the record, so the newest verdict is never read as a block.
func latestJudgeOutcome(p *PRD) string {
	if p != nil && len(p.Verdicts) > 0 && p.Verdicts[len(p.Verdicts)-1].Outcome == "warn" {
		return "warn"
	}
	return "pass"
}

// checkCompletionLanding runs the gate for one completion path. When it
// refuses, it records the reason on the Automaton (CompletionRefusal and a
// decision line) and returns false; the status is not touched. When it
// allows, it clears a stale refusal and returns true.
func (m *Manager) checkCompletionLanding(ctx context.Context, p *PRD, path, judge, extraCommit string) (bool, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	enabled, git, repo := m.completionLandingSettings()
	if judge == "" {
		judge = latestJudgeOutcome(p)
	}
	if repo == "" && p != nil {
		repo = strings.TrimSpace(p.ProjectDir)
	}
	d := EvaluateCompletionLanding(ctx, enabled, git, repo, p, judge, extraCommit)
	if d.Allowed {
		if p != nil && p.CompletionRefusal != "" {
			m.store.WithLock(func() { p.CompletionRefusal = "" })
		}
		return true, d.Reason
	}
	if p == nil {
		return false, d.Reason
	}
	m.store.WithLock(func() {
		if p.CompletionRefusal == d.Reason {
			return
		}
		p.CompletionRefusal = d.Reason
		p.Decisions = append(p.Decisions, Decision{
			At: time.Now(), Kind: DecisionCompletionRefused, Actor: "completion_landing",
			Note: "path " + path + ": " + d.Reason,
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		return false, d.Reason + " (record not saved: " + err.Error() + ")"
	}
	return false, d.Reason
}
