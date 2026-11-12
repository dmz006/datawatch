package mergeshadow

import (
	"context"
	"fmt"
	"strings"
)

// StrayRootFileCode is the finding code for a file that the Automaton branch
// adds at the repository root while the default branch does not track it. Such
// a file is almost always a leftover from a task's working directory, and a
// merge would land it in the root of the default branch.
const StrayRootFileCode = "stray_root_file"

// CheckStrayRootFiles returns one blocking finding for each file the branch
// adds at the repository root that is not already tracked at the root of
// baseRef (the default branch). Files added in subdirectories, and root files
// that baseRef already tracks, produce no finding. The findings are returned in
// path order; none means the branch adds no stray root file.
//
// Nothing is changed in the repository. A git failure is an error.
func CheckStrayRootFiles(ctx context.Context, repoPath, branch, baseRef string) ([]Finding, error) {
	if repoPath == "" || branch == "" || baseRef == "" {
		return nil, fmt.Errorf("mergeshadow: repo path, branch and base ref are required")
	}
	for _, name := range []string{branch, baseRef} {
		if strings.HasPrefix(name, "-") {
			return nil, fmt.Errorf("mergeshadow: invalid ref name %q", name)
		}
	}

	// Three-dot form diffs from the merge base, so only files the branch
	// itself added are listed, not files the base gained since it forked.
	out, err := runGit(ctx, repoPath, "diff", "-z", "--name-only", "--diff-filter=A",
		"refs/heads/"+baseRef+"...refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("mergeshadow: list files added by %s: %w", branch, err)
	}

	var findings []Finding
	for _, path := range strings.Split(out, "\x00") {
		if path == "" || strings.Contains(path, "/") {
			continue
		}
		tracked, err := trackedAtRoot(ctx, repoPath, baseRef, path)
		if err != nil {
			return nil, err
		}
		if tracked {
			continue
		}
		findings = append(findings, Finding{
			Code:     StrayRootFileCode,
			Message:  path + " is added at the repository root by " + branch + " but is not tracked on " + baseRef,
			Blocking: true,
		})
	}
	return findings, nil
}

// trackedAtRoot reports whether baseRef tracks a file named name at its root.
func trackedAtRoot(ctx context.Context, repoPath, baseRef, name string) (bool, error) {
	out, err := runGit(ctx, repoPath, "ls-tree", "--name-only", "-z",
		"refs/heads/"+baseRef, "--", name)
	if err != nil {
		return false, fmt.Errorf("mergeshadow: check %s on %s: %w", name, baseRef, err)
	}
	return strings.Trim(out, "\x00") != "", nil
}
