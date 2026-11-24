// Unlanded work classifier. ListUnlandedBranches looks at the archived
// Automata whose automaton/* branch carries commits that are not on the
// default branch and recommends what to do with each one. It compares by
// commit content (git cherry, which matches patch-ids), never by subject, so
// a merge that rewrote commits (a rebase or renumbering) still counts as
// landed. It is strictly read-only. Running Automata are excluded because
// their commits are expected to be ahead of the default branch.
package autonomous

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Recommendations for an unlanded branch.
const (
	// UnlandedLanded: every commit is already on the default branch by
	// content, so the branch is a leftover.
	UnlandedLanded = "landed"
	// UnlandedSuperseded: some commits are not on the default branch, but
	// every file they touch was changed on the default branch since the
	// branch forked, so later work replaced them.
	UnlandedSuperseded = "superseded"
	// UnlandedNeedsDecision: commits are not on the default branch and
	// nothing shows they were replaced; the operator must decide.
	UnlandedNeedsDecision = "needs_decision"
)

// UnlandedBranch is one archived Automaton branch reviewed for landing.
type UnlandedBranch struct {
	AutomatonID string `json:"automaton_id"`
	Branch      string `json:"branch"`
	// Title is the Automaton's title, falling back to the first line of its
	// spec, so the list says what the work was for.
	Title string `json:"title"`
	// Commits is the number of commits whose content is not on the default
	// branch (zero is never listed unless the branch is ahead only by
	// rewritten commits, in which case it is zero and the branch is landed).
	Commits int `json:"commits"`
	// LastActivity is the committer time of the branch tip.
	LastActivity   time.Time `json:"last_activity"`
	Recommendation string    `json:"recommendation"`
	Reason         string    `json:"reason"`
}

// ListUnlandedBranches reports the archived Automata in prds whose branch in
// baseRepo has commits ahead of the default branch. Branches with no commits
// ahead, branches that no longer exist and Automata that are not archived are
// left out. The result is ordered with the least settled first (needs a
// decision, superseded, landed), then newest activity first.
func ListUnlandedBranches(ctx context.Context, git MergeGitRunner, baseRepo string, prds []*PRD) ([]UnlandedBranch, error) {
	base, err := defaultBranchForRepo(ctx, git, baseRepo)
	if err != nil {
		return nil, fmt.Errorf("unlanded work: %w", err)
	}
	out := []UnlandedBranch{}
	for _, p := range prds {
		if p == nil || NormalizePRDStatus(p.Status) != PRDArchived {
			continue
		}
		if landingDecided(p) {
			continue // the operator kept or dropped this branch on record
		}
		branch := p.Git.Branch
		if branch == "" {
			branch = automatonBranchPrefix + p.ID
		}
		if branch == base {
			continue
		}
		if _, err := git.Run(ctx, baseRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
			continue // branch already gone
		}
		ahead, err := git.Run(ctx, baseRepo, "rev-list", "--count", base+".."+branch)
		if err != nil || strings.TrimSpace(ahead) == "0" {
			continue
		}
		cherry, err := git.Run(ctx, baseRepo, "cherry", base, branch)
		if err != nil {
			return nil, fmt.Errorf("unlanded work: cherry %s: %w", branch, err)
		}
		unlanded := 0
		for _, line := range strings.Split(cherry, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "+") {
				unlanded++
			}
		}
		e := UnlandedBranch{
			AutomatonID: p.ID, Branch: branch, Title: unlandedTitle(p), Commits: unlanded,
		}
		if ts, err := git.Run(ctx, baseRepo, "log", "-1", "--format=%cI", branch); err == nil {
			e.LastActivity, _ = time.Parse(time.RFC3339, strings.TrimSpace(ts))
		}
		e.Recommendation, e.Reason = classifyUnlanded(ctx, git, baseRepo, base, branch, unlanded)
		if e.Recommendation != UnlandedLanded {
			// A hand port shares no commit with its branch, so the commit
			// comparison above cannot see it. The shipped record, or the
			// same file content on the default branch, proves it landed.
			if why, ok := deliveredOnBase(ctx, git, baseRepo, base, branch, p); ok {
				e.Recommendation, e.Reason = UnlandedLanded, why
			}
		}
		out = append(out, e)
	}
	rank := map[string]int{UnlandedNeedsDecision: 0, UnlandedSuperseded: 1, UnlandedLanded: 2}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Recommendation] != rank[out[j].Recommendation] {
			return rank[out[i].Recommendation] < rank[out[j].Recommendation]
		}
		return out[i].LastActivity.After(out[j].LastActivity)
	})
	return out, nil
}

func unlandedTitle(p *PRD) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	s := strings.TrimSpace(p.Spec)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func classifyUnlanded(ctx context.Context, git MergeGitRunner, repo, base, branch string, unlanded int) (string, string) {
	if unlanded == 0 {
		return UnlandedLanded, "every commit is already on " + base + " by content"
	}
	needs := func(why string) (string, string) {
		return UnlandedNeedsDecision, fmt.Sprintf("%d commits are not on %s%s", unlanded, base, why)
	}
	mb, err := git.Run(ctx, repo, "merge-base", base, branch)
	if err != nil {
		return needs("; no common history to compare")
	}
	mb = strings.TrimSpace(mb)
	branchFiles, err := git.Run(ctx, repo, "diff", "--name-only", mb, branch)
	if err != nil {
		return needs("")
	}
	baseFiles, err := git.Run(ctx, repo, "diff", "--name-only", mb, base)
	if err != nil {
		return needs("")
	}
	changed := map[string]bool{}
	for _, f := range strings.Split(baseFiles, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			changed[f] = true
		}
	}
	n := 0
	for _, f := range strings.Split(branchFiles, "\n") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n++
		if !changed[f] {
			return needs("; nothing shows it was replaced")
		}
	}
	if n == 0 {
		return needs("")
	}
	return UnlandedSuperseded, fmt.Sprintf("%d commits are not on %s, but all %d files they change were changed on %s since the branch forked", unlanded, base, n, base)
}

// landingDecided reports whether the Automaton's landing record carries an
// operator decision (kept or dropped). Those branches are settled on record,
// with actor and reason, so they are not listed as work waiting to land.
func landingDecided(p *PRD) bool {
	return p != nil && p.Landing != nil &&
		(p.Landing.State == LandingKept || p.Landing.State == LandingDropped)
}

var shippedCommitRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// maxContentMatchFiles bounds the content comparison: a branch that changed
// more files than this is left to the operator.
const maxContentMatchFiles = 500

// deliveredOnBase says whether the work of an archived Automaton's branch is
// already on the default branch although git cherry sees no matching commit
// (a hand port). Two proofs, both read-only:
//
//  1. the Automaton's shipped record (release and merge commit) names a commit
//     that is an ancestor of the default branch;
//  2. every file the branch changed since it forked has identical content at
//     the branch tip and on the default branch.
//
// Anything else, including a git error, is not proof.
func deliveredOnBase(ctx context.Context, git MergeGitRunner, repo, base, branch string, p *PRD) (string, bool) {
	if hasShippedRecord(p) {
		commit := strings.TrimSpace(p.MergeCommit)
		if shippedCommitRe.MatchString(commit) {
			if _, err := git.Run(ctx, repo, "merge-base", "--is-ancestor", commit, base); err == nil {
				return fmt.Sprintf("delivered: its shipped record (%s, commit %s) is on %s", strings.TrimSpace(p.MergedInRelease), shortCommit(commit), base), true
			}
		}
	}
	mb, err := git.Run(ctx, repo, "merge-base", base, branch)
	if err != nil {
		return "", false
	}
	out, err := git.Run(ctx, repo, "diff", "--name-only", strings.TrimSpace(mb), branch)
	if err != nil {
		return "", false
	}
	var files []string
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 || len(files) > maxContentMatchFiles {
		return "", false
	}
	args := append([]string{"diff", "--quiet", base, branch, "--"}, files...)
	if _, err := git.Run(ctx, repo, args...); err != nil {
		return "", false // differs (exit 1) or could not compare: not proven
	}
	return fmt.Sprintf("delivered by content: all %d files the branch changed have the same content on %s", len(files), base), true
}
