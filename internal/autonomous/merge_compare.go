// Automaton merge-pending comparison. Given an Automata work branch and
// the base branch it was meant to land on, CompareBranchToMain classifies
// the branch as merged, pending, needs_look, empty or unknown using only
// read-only git queries (rev-list, cherry, log). It never deletes a
// branch, removes a worktree, pushes, resets, checks out or merges.
package autonomous

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Merge comparison states reported by CompareBranchToMain.
const (
	MergeStateMerged    = "merged"
	MergeStatePending   = "pending"
	MergeStateNeedsLook = "needs_look"
	MergeStateEmpty     = "empty"
	MergeStateUnknown   = "unknown"
)

// MergeGitRunner runs one git command in dir and returns its output. The
// production implementation wraps runGitInCtx; decision tests substitute a fake.
type MergeGitRunner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// shellMergeGit is the production MergeGitRunner.
type shellMergeGit struct{}

// NewMergeGitRunner returns the production runner, which shells out to git
// through runGitInCtx.
func NewMergeGitRunner() MergeGitRunner { return shellMergeGit{} }

// Run implements MergeGitRunner. The git process is killed when ctx ends, so
// a hung git cannot outlive the caller's deadline.
func (shellMergeGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitInCtx(ctx, dir, args...)
}

// MergeCompare is the result of comparing an Automaton branch with its base.
type MergeCompare struct {
	// Ahead is the number of commits on the branch that are not on base.
	Ahead int `json:"ahead"`
	// CherryNotOnBase counts branch commits whose patch is absent from base
	// (git cherry lines beginning with plus).
	CherryNotOnBase int `json:"cherry_not_on_base"`
	// CherryOnBase counts branch commits whose patch is already on base
	// (git cherry lines beginning with minus).
	CherryOnBase int `json:"cherry_on_base"`
	// SubjectsOnBase is true when every commit subject of base..branch
	// appears as an exact line of the base log; true for no commits.
	SubjectsOnBase bool `json:"subjects_on_base"`
	// State is one of the MergeState constants.
	State string `json:"state"`
	// Error holds the git error text when State is unknown.
	Error string `json:"error,omitempty"`
}

// CompareBranchToMain classifies branch against base in repo. It issues only
// read-only git commands. Any git error yields State unknown with the error
// text kept in the Error field.
func CompareBranchToMain(ctx context.Context, git MergeGitRunner, repo, branch, base string) MergeCompare {
	res := MergeCompare{State: MergeStateUnknown}
	fail := func(what string, out string, err error) MergeCompare {
		res.State = MergeStateUnknown
		res.Error = fmt.Sprintf("Automaton merge compare: %s: %v: %s", what, err, strings.TrimSpace(out))
		return res
	}

	rng := base + ".." + branch

	out, err := git.Run(ctx, repo, "rev-list", "--count", rng)
	if err != nil {
		return fail("rev-list", out, err)
	}
	ahead, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return fail("rev-list count", out, convErr)
	}
	res.Ahead = ahead

	out, err = git.Run(ctx, repo, "cherry", base, branch)
	if err != nil {
		return fail("cherry", out, err)
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			res.CherryNotOnBase++
		case strings.HasPrefix(line, "-"):
			res.CherryOnBase++
		}
	}

	branchSubjects, err := git.Run(ctx, repo, "log", rng, "--format=%s")
	if err != nil {
		return fail("log branch subjects", branchSubjects, err)
	}
	baseSubjects, err := git.Run(ctx, repo, "log", base, "--format=%s")
	if err != nil {
		return fail("log base subjects", baseSubjects, err)
	}
	onBase := make(map[string]struct{})
	for _, s := range strings.Split(baseSubjects, "\n") {
		onBase[s] = struct{}{}
	}
	res.SubjectsOnBase = true
	for _, s := range strings.Split(branchSubjects, "\n") {
		if s == "" {
			continue
		}
		if _, ok := onBase[s]; !ok {
			res.SubjectsOnBase = false
			break
		}
	}

	res.State = decideMergeState(res)
	return res
}

// decideMergeState applies the Automaton merge decision rules, in order.
func decideMergeState(c MergeCompare) string {
	switch {
	case c.Ahead == 0:
		return MergeStateEmpty
	case c.CherryNotOnBase == 0 && c.SubjectsOnBase:
		return MergeStateMerged
	case c.CherryNotOnBase == 0:
		return MergeStateNeedsLook
	case !c.SubjectsOnBase:
		return MergeStatePending
	default:
		return MergeStateNeedsLook
	}
}
