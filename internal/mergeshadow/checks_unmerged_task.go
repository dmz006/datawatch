package mergeshadow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// UnmergedTaskBranchCode is the finding code for a task branch of an
// Automaton that holds commits the Automaton branch does not contain. Such a
// task's work would be silently dropped by a merge of the Automaton branch.
const UnmergedTaskBranchCode = "unmerged_task_branch"

// taskBranchSeparator separates an Automaton branch from the suffix of each of
// its task branches: "<automaton branch>--tasks-<task slug>".
const taskBranchSeparator = "--tasks-"

// CheckUnmergedTaskBranches lists the task branches of the Automaton branch
// branch in repoPath and returns one blocking finding for each task branch
// that has commits not reachable from the Automaton branch. A task branch
// whose commits are all reachable from it (already merged) produces no
// finding. The findings are returned in ref-name order; none means no task
// branch holds unmerged work.
//
// Nothing is changed in the repository. A git failure is an error.
func CheckUnmergedTaskBranches(ctx context.Context, repoPath, branch string) ([]Finding, error) {
	if repoPath == "" || branch == "" {
		return nil, fmt.Errorf("mergeshadow: repo path and branch are required")
	}
	if strings.HasPrefix(branch, "-") {
		return nil, fmt.Errorf("mergeshadow: invalid branch name %q", branch)
	}
	prefix := branch + taskBranchSeparator
	refs, err := runGit(ctx, repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("mergeshadow: list task branches: %w", err)
	}

	var out []Finding
	for _, name := range strings.Split(strings.TrimSpace(refs), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		count, err := unmergedCommitCount(ctx, repoPath, name, branch)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			continue
		}
		out = append(out, Finding{
			Code: UnmergedTaskBranchCode,
			Message: name + " has " + strconv.Itoa(count) +
				" commit(s) not on " + branch,
			Blocking: true,
		})
	}
	return out, nil
}

// unmergedCommitCount counts the commits reachable from task that are not
// reachable from branch. Both names are resolved as local branches.
func unmergedCommitCount(ctx context.Context, repoPath, task, branch string) (int, error) {
	out, err := runGit(ctx, repoPath, "rev-list", "--count",
		"refs/heads/"+task, "--not", "refs/heads/"+branch)
	if err != nil {
		return 0, fmt.Errorf("mergeshadow: count commits of %s: %w", task, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("mergeshadow: parse commit count of %s: %w", task, err)
	}
	return n, nil
}
