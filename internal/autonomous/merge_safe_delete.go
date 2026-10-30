// Automaton safe-to-delete report. SafeToDeleteReport lists the local
// automaton/* work branches (and their worktrees) whose commits are all on
// the base branch, so an operator can review what is safe to clean up. It is
// strictly read-only: it never deletes a branch, removes a worktree, pushes
// or resets; deletion stays a separate, operator-approved step.
package autonomous

import (
	"context"
	"fmt"
	"strings"
)

// Reasons attached to a safe entry.
const (
	safeReasonMerged = "all commits on main"
	safeReasonEmpty  = "no commits ahead of main"
)

// automatonBranchPrefix is the ref namespace of Automata work branches.
const automatonBranchPrefix = "automaton/"

// SafeDeleteEntry is one Automaton branch that is safe to delete.
type SafeDeleteEntry struct {
	Branch string `json:"branch"`
	// Worktree is the checked-out worktree path, empty when none exists.
	Worktree string `json:"worktree"`
	Reason   string `json:"reason"`
	// Commits is the number of commits on the branch that are not on base.
	Commits int `json:"commits"`
}

// SafeDeleteHeld is one Automaton branch that must not be deleted yet.
type SafeDeleteHeld struct {
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
	// State is the merge comparison state (pending, needs_look, unknown, or
	// merged/empty for a held dirty worktree).
	State string `json:"state"`
	// Dirty is true when the worktree has uncommitted changes.
	Dirty bool   `json:"dirty,omitempty"`
	Note  string `json:"note,omitempty"`
}

// SafeDeleteReport is the result of SafeToDeleteReport.
type SafeDeleteReport struct {
	Safe []SafeDeleteEntry `json:"safe"`
	Held []SafeDeleteHeld  `json:"held"`
	// HeldDirty counts safe-by-state branches held for uncommitted changes.
	HeldDirty int `json:"held_dirty"`
}

// SafeToDeleteReport inspects the Automaton branches of baseRepo against
// base and reports which are safe to delete. The base branch and the branch
// checked out in the primary checkout are skipped. A listing failure returns
// an error; a per-branch git failure holds that branch as unknown.
func SafeToDeleteReport(ctx context.Context, git MergeGitRunner, baseRepo, base string) (SafeDeleteReport, error) {
	report := SafeDeleteReport{Safe: []SafeDeleteEntry{}, Held: []SafeDeleteHeld{}}

	out, err := git.Run(ctx, baseRepo, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+automatonBranchPrefix)
	if err != nil {
		return report, fmt.Errorf("automaton safe-delete: list branches: %w: %s", err, strings.TrimSpace(out))
	}
	wtOut, err := git.Run(ctx, baseRepo, "worktree", "list", "--porcelain")
	if err != nil {
		return report, fmt.Errorf("automaton safe-delete: list worktrees: %w: %s", err, strings.TrimSpace(wtOut))
	}
	worktrees, primaryBranch := parseWorktreeList(wtOut)

	for _, branch := range strings.Split(out, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == base || branch == primaryBranch {
			continue
		}
		wt := worktrees[branch]
		cmp := CompareBranchToMain(ctx, git, baseRepo, branch, base)

		var reason string
		switch cmp.State {
		case MergeStateMerged:
			reason = safeReasonMerged
		case MergeStateEmpty:
			reason = safeReasonEmpty
		default:
			held := SafeDeleteHeld{Branch: branch, Worktree: wt, State: cmp.State}
			if cmp.Error != "" {
				held.Note = cmp.Error
			}
			report.Held = append(report.Held, held)
			continue
		}

		if wt != "" {
			status, serr := git.Run(ctx, baseRepo, "-C", wt, "status", "--porcelain")
			if serr != nil {
				report.Held = append(report.Held, SafeDeleteHeld{
					Branch: branch, Worktree: wt, State: MergeStateUnknown,
					Note: fmt.Sprintf("automaton safe-delete: worktree status: %v: %s", serr, strings.TrimSpace(status)),
				})
				continue
			}
			if strings.TrimSpace(status) != "" {
				report.Held = append(report.Held, SafeDeleteHeld{
					Branch: branch, Worktree: wt, State: cmp.State, Dirty: true,
					Note: "Automaton worktree has uncommitted changes",
				})
				report.HeldDirty++
				continue
			}
		}
		report.Safe = append(report.Safe, SafeDeleteEntry{
			Branch: branch, Worktree: wt, Reason: reason, Commits: cmp.Ahead,
		})
	}
	return report, nil
}

// parseWorktreeList reads git worktree list --porcelain output. It returns a
// map of branch short name to worktree path for every linked worktree, and
// the branch checked out in the primary checkout (the first entry).
func parseWorktreeList(out string) (map[string]string, string) {
	byBranch := make(map[string]string)
	primary := ""
	var path, branch string
	first := true
	flush := func() {
		if path == "" {
			return
		}
		if first {
			primary = branch
			first = false
		} else if branch != "" {
			byBranch[branch] = path
		}
		path, branch = "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return byBranch, primary
}
