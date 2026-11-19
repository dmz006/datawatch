package autonomous

import "context"

// Tests that finish Automata on fake worktree branches have no repository to
// ask; their default checker reports every branch merged. Gate behaviour is
// tested in completion_landing_test.go with explicit fakes.
type alwaysMergedLandingGit struct{}

func (alwaysMergedLandingGit) BranchMerged(context.Context, string, string) (bool, string) {
	return true, ""
}

func init() {
	completionLandingGitDefault = func() CompletionLandingGit { return alwaysMergedLandingGit{} }
}
