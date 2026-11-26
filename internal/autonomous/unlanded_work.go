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
		branch := p.Git.Branch
		if branch == "" {
			branch = automatonBranchPrefix + p.ID
		}
		if branch == base {
			continue
		}
		tipOut, err := git.Run(ctx, baseRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		if err != nil {
			continue // branch already gone
		}
		// A kept or dropped landing decision settles the branch only while it
		// still points at the commit the decision was made at. Without a
		// recorded tip the decision is honoured as before.
		moved := false
		if landingDecided(p) {
			if p.Landing.Tip == "" || p.Landing.Tip == strings.TrimSpace(tipOut) {
				continue
			}
			moved = true
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
		if moved && e.Recommendation != UnlandedLanded {
			e.Reason = fmt.Sprintf("new commits after the decision (%s by %s): %s", p.Landing.State, p.Landing.Actor, e.Reason)
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
// with actor and reason, so they are not listed as work waiting to land, until
// the branch moves past the tip recorded with the decision.
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
//  2. every file the branch's own commits changed has identical content (the
//     same blob) at the branch tip and on the default branch.
//
// Anything else, including a git error, is not proof.
func deliveredOnBase(ctx context.Context, git MergeGitRunner, repo, base, branch string, p *PRD) (string, bool) {
	if hasShippedRecord(p) {
		commit := strings.TrimSpace(p.MergeCommit)
		if shippedCommitRe.MatchString(commit) {
			if arg, err := storedCommitArg(ctx, git, repo, commit); err == nil {
				if _, err := git.Run(ctx, repo, "merge-base", "--is-ancestor", "--end-of-options", arg, base); err == nil {
					return fmt.Sprintf("delivered: its shipped record (%s, commit %s) is on %s", strings.TrimSpace(p.MergedInRelease), shortCommit(commit), base), true
				}
			}
		}
	}
	files := branchChangedFiles(ctx, git, repo, base, branch)
	if len(files) == 0 || len(files) > maxContentMatchFiles {
		return "", false
	}
	// Same blob at the branch tip and on the base for every changed file.
	for start := 0; start < len(files); start += contentMatchChunk {
		end := start + contentMatchChunk
		if end > len(files) {
			end = len(files)
		}
		chunk := files[start:end]
		args := []string{"rev-parse"}
		for _, f := range chunk {
			args = append(args, base+":"+f, branch+":"+f)
		}
		out, err := git.Run(ctx, repo, args...)
		if err != nil {
			return "", false // a file is missing on one side, or git failed: not proven
		}
		ids := strings.Fields(out)
		if len(ids) != 2*len(chunk) {
			return "", false
		}
		for k := 0; k < len(ids); k += 2 {
			if ids[k] != ids[k+1] {
				return "", false
			}
		}
	}
	return fmt.Sprintf("delivered by content: all %d files the branch changed have the same content on %s", len(files), base), true
}

// contentMatchChunk is how many files one rev-parse call compares.
const contentMatchChunk = 100

// branchChangedFiles lists the paths touched by the commits the branch has
// that the base lacks (renames listed as delete plus add, so both paths are
// compared). Read-only (git log). Nil on any error.
func branchChangedFiles(ctx context.Context, git MergeGitRunner, repo, base, branch string) []string {
	out, err := git.Run(ctx, repo, "log", "--no-renames", "--name-only", "--format=", "-z", base+".."+branch)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range strings.FieldsFunc(out, func(r rune) bool { return r == 0 || r == '\n' }) {
		if f = strings.TrimSpace(f); f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files
}

// checkBranchAheadDelivered is checkBranchAhead plus the delivery proof of
// deliveredOnBase: a branch the comparison finds ahead of the base, whose work
// is already on the base (the record's shipped record, or the same file
// content), reads as merged, with the proof in detail. p may be nil, in which
// case only the content proof applies. The close-step guard, the carry-forward
// and the unarchive check keep using checkBranchAhead, which is unchanged.
func checkBranchAheadDelivered(ctx context.Context, git MergeGitRunner, repo, branch, base string, p *PRD) (state string, ahead int, detail string) {
	state, ahead, detail = checkBranchAhead(ctx, git, repo, branch, base)
	if state != branchCheckAhead {
		return state, ahead, detail
	}
	if base == "" {
		base = resolveMergeBase(ctx, git, repo)
	}
	if base == "" {
		return state, ahead, detail
	}
	if why, ok := deliveredOnBase(ctx, git, repo, base, branch, p); ok {
		return branchCheckMerged, 0, why
	}
	return state, ahead, detail
}
