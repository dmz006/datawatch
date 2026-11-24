package mergeshadow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// holderPrefix starts the name of the folder that holds one trial worktree.
const holderPrefix = "trial-merge-"

// SweepScratch removes the trial worktrees left under dataDir by a daemon that
// stopped during a trial: TrialMergeWith removes its own on every exit it
// lives to see. Call it only when no trial of this data directory can be
// running, which for the daemon is at start.
//
// Only the trial holder folders of the scratch folder are touched; a link or a
// file is left alone whatever its name. A worktree is unregistered from the
// repository that owns it before its folder is deleted. It returns the holders
// removed; one that cannot be removed does not stop the others, and the first
// such error is returned. A missing scratch folder is not an error.
func SweepScratch(ctx context.Context, dataDir string) (removed []string, err error) {
	if dataDir == "" {
		return nil, nil
	}
	scratch, err := filepath.Abs(filepath.Join(dataDir, scratchDirName))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(scratch)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	repos := map[string]bool{}
	var firstErr error
	for _, e := range entries { // ReadDir sorts by name
		if !e.IsDir() || !strings.HasPrefix(e.Name(), holderPrefix) {
			continue
		}
		if cerr := ctx.Err(); cerr != nil {
			firstErr = cerr
			break
		}
		holder := filepath.Join(scratch, e.Name())
		wt := filepath.Join(holder, "wt")
		if repo := worktreeOwner(ctx, wt); repo != "" {
			repos[repo] = true
			_, _ = runGit(ctx, repo, "worktree", "remove", "--force", wt)
		}
		if rerr := os.RemoveAll(holder); rerr != nil {
			if firstErr == nil {
				firstErr = rerr
			}
			continue
		}
		removed = append(removed, holder)
	}
	names := make([]string, 0, len(repos))
	for r := range repos {
		names = append(names, r)
	}
	sort.Strings(names)
	for _, r := range names {
		// Drops the registration where the removal above could not.
		_, _ = runGit(ctx, r, "worktree", "prune")
	}
	return removed, firstErr
}

// worktreeOwner returns the directory to run git in for the repository that
// owns the linked worktree at wt, or "" when wt is not a linked worktree. The
// .git file is required first: in a half-made folder git would otherwise
// answer for whatever repository happens to surround the data directory.
func worktreeOwner(ctx context.Context, wt string) string {
	if st, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	out, err := runGit(ctx, wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
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
	return common // a bare repository
}
