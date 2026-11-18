// merge_shadow_sweep.go — removes merge trial worktrees left behind.
//
// A trial merge builds a detached worktree in a holder folder under the merge
// shadow scratch folder. The trial removes it when it ends; a daemon that dies
// mid-trial cannot, so the holder and the worktree registration stay behind.
// The daemon sweeps them once at start (API.StaleTrialSweep): no trial can be
// running then, so every holder under the scratch folder is stale.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// mergeShadowScratchDir is the folder under the data directory that holds the
// trial worktrees. It matches the mergeshadow package's scratch folder.
const mergeShadowScratchDir = "merge-shadow-scratch"

// mergeShadowHolderPrefix starts the name of every trial holder folder.
const mergeShadowHolderPrefix = "trial-merge-"

// SweepMergeShadowScratch removes every trial holder folder under the merge
// shadow scratch folder of dataDir, unregistering its worktree from the
// repository that owns it first. It returns the holders removed. A missing
// scratch folder is not an error. Only holder folders are touched.
func SweepMergeShadowScratch(ctx context.Context, git MergeGitRunner, dataDir string) ([]string, error) {
	if dataDir == "" {
		return nil, nil
	}
	scratch := filepath.Join(dataDir, mergeShadowScratchDir)
	entries, err := os.ReadDir(scratch)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	repos := map[string]bool{}
	var firstErr error
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), mergeShadowHolderPrefix) {
			continue
		}
		holder := filepath.Join(scratch, e.Name())
		wt := filepath.Join(holder, "wt")
		if repo := trialWorktreeRepo(ctx, git, wt); repo != "" {
			repos[repo] = true
			_, _ = git.Run(ctx, repo, "worktree", "remove", "--force", wt)
		}
		if err := os.RemoveAll(holder); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, holder)
	}
	for repo := range repos {
		_, _ = git.Run(ctx, repo, "worktree", "prune")
	}
	return removed, firstErr
}

// trialWorktreeRepo returns the working directory of the repository that owns
// the worktree at wt, or "" when wt is gone or not a worktree.
func trialWorktreeRepo(ctx context.Context, git MergeGitRunner, wt string) string {
	if st, err := os.Stat(wt); err != nil || !st.IsDir() {
		return ""
	}
	out, err := git.Run(ctx, wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return ""
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common // bare repository
}

// PruneTrialWorktrees removes the detached trial worktrees of repo that live
// under scratchDir, then prunes dangling registrations. A worktree whose path
// is in live (the worktrees of live Automata), one on a branch, and one
// outside scratchDir are never removed. It returns the paths removed.
func PruneTrialWorktrees(ctx context.Context, git MergeGitRunner, repo, scratchDir string, live []string) ([]string, error) {
	out, err := git.Run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	liveSet := map[string]bool{}
	for _, p := range live {
		if p != "" {
			liveSet[filepath.Clean(p)] = true
		}
	}
	scratch := filepath.Clean(scratchDir) + string(filepath.Separator)
	var removed []string
	for _, block := range strings.Split(out, "\n\n") {
		var path string
		detached := false
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				path = filepath.Clean(strings.TrimPrefix(line, "worktree "))
			case line == "detached":
				detached = true
			}
		}
		if path == "" || !detached || liveSet[path] || !strings.HasPrefix(path, scratch) {
			continue
		}
		if _, err := git.Run(ctx, repo, "worktree", "remove", "--force", path); err != nil {
			continue
		}
		removed = append(removed, path)
	}
	_, _ = git.Run(ctx, repo, "worktree", "prune")
	return removed, nil
}

// liveAutomatonWorktrees lists the worktree paths recorded on Automata that
// have not reached a terminal state.
func (m *Manager) liveAutomatonWorktrees() (paths, repos []string) {
	seen := map[string]bool{}
	for _, prd := range m.store.ListPRDs() {
		if prd == nil || prd.ProjectDir == "" {
			continue
		}
		if !seen[prd.ProjectDir] {
			seen[prd.ProjectDir] = true
			repos = append(repos, prd.ProjectDir)
		}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if p := prd.Story[si].Tasks[ti].WorktreePath; p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths, repos
}
