// Automaton safe-to-delete report. SafeToDeleteReport lists the local
// automaton/* work branches (and their worktrees) whose commits are all on
// the base branch, so an operator can review what is safe to clean up. It
// is strictly read-only: it never deletes a branch, removes a worktree,
// pushes or resets; deletion stays a separate, operator-approved step.
//
// The report makes a fixed number of git calls for listing: one for-each-ref
// (carrying each branch's ahead count via the ahead-behind atom), one
// worktree list, and — only when at least one branch is ahead — one base
// subject log. Per branch it then makes at most two calls (cherry, branch
// subject log) plus — unless the branch's Automaton is running — a status
// check for safe branches that own a worktree, running the per-branch work
// through a bounded worker pool.
package autonomous

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Reasons attached to a safe entry; they name the project's own default branch.
func safeReasonMerged(base string) string { return "all commits on " + base }
func safeReasonEmpty(base string) string  { return "no commits ahead of " + base }

// automatonBranchPrefix is the ref namespace of Automata work branches.
const automatonBranchPrefix = "automaton/"

// safeDeleteGitConcurrency bounds the number of goroutines the report runs
// for per-branch git work (cherry, branch subject log, worktree status).
const safeDeleteGitConcurrency = 4

// SafeDeleteEntry is one Automaton branch that is safe to delete.
type SafeDeleteEntry struct {
	Branch string `json:"branch"`
	// Worktree is the checked-out worktree path, empty when none exists.
	Worktree string `json:"worktree"`
	Reason   string `json:"reason"`
	// Commits is the number of commits on the branch that are not on base.
	Commits int `json:"commits"`
	// DirtyCheckSkipped is true when the worktree's uncommitted-changes
	// check was skipped because the branch's Automaton is still running.
	DirtyCheckSkipped bool `json:"dirty_check_skipped,omitempty"`
}

// SafeDeleteOptions carries the per-Automaton context the safe-to-delete
// report needs beyond the git repository.
type SafeDeleteOptions struct {
	// IsRunning reports whether the branch's Automaton is still running.
	// For those worktrees the report skips the uncommitted-changes status
	// read (a running Automaton's worktree is expected to churn) and marks
	// the entry with DirtyCheckSkipped. Nil treats no Automaton as running.
	IsRunning func(branch string) bool
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
	// ComputedAt is when the report was computed (RFC 3339, UTC).
	// Set by the safe-to-delete cache; emitted always.
	ComputedAt string `json:"computed_at"`
	// Stale is true when the report is older than the cache lifetime;
	// emitted always (false for reports served fresh).
	Stale bool `json:"stale"`
}

// sdPlan is one candidate Automaton branch after filtering out base and the
// primary checkout branch: its name, its worktree (empty when none), the
// number of commits the branch is ahead of base, and an error when neither
// listing path produced a count.
type sdPlan struct {
	branch string
	wt     string
	ahead  int
	err    error
}

// sdResult is the per-branch outcome of SafeToDeleteReport: the merge
// classification plus the worktree status check result for safe branches
// that own a worktree. skipped means the branch's Automaton is running,
// so the status read was deliberately not made.
type sdResult struct {
	cmp       MergeCompare
	statusErr error
	statusOut string
	dirty     bool
	skipped   bool
}

// SafeToDeleteReport inspects the Automaton branches of baseRepo against
// base and reports which are safe to delete. The base branch and the branch
// checked out in the primary checkout are skipped. The ahead-behind atom in
// for-each-ref needs git 2.36 or newer; if the call fails the report falls
// back to a plain ref listing plus one rev-list --count call per branch. A
// listing failure returns an error; a per-branch git failure holds that
// branch as unknown.
func SafeToDeleteReport(ctx context.Context, git MergeGitRunner, baseRepo, base string, opts SafeDeleteOptions) (SafeDeleteReport, error) {
	report := SafeDeleteReport{Safe: []SafeDeleteEntry{}, Held: []SafeDeleteHeld{}}

	// Fixed-cost listing: one for-each-ref that prints each branch name and
	// its behind/ahead counts against base (needs git >= 2.36).
	var branchNames []string
	aheads := map[string]int{}
	fellBack := false
	out, err := git.Run(ctx, baseRepo,
		"for-each-ref", "--format=%(refname:short) %(ahead-behind:"+base+")",
		"refs/heads/"+automatonBranchPrefix)
	if err == nil {
		ok := true
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 3 { // name <behind> <ahead>
				ok = false
				break
			}
			n, convErr := strconv.Atoi(fields[2])
			if convErr != nil {
				ok = false
				break
			}
			aheads[fields[0]] = n
			branchNames = append(branchNames, fields[0])
		}
		if !ok {
			fellBack = true
			aheads, branchNames = nil, nil
		}
	} else {
		fellBack = true
	}
	if fellBack {
		out, err = git.Run(ctx, baseRepo,
			"for-each-ref", "--format=%(refname:short)",
			"refs/heads/"+automatonBranchPrefix)
		if err != nil {
			return report, fmt.Errorf("automaton safe-delete: list branches: %w: %s", err, strings.TrimSpace(out))
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				branchNames = append(branchNames, line)
			}
		}
	}

	wtOut, err := git.Run(ctx, baseRepo, "worktree", "list", "--porcelain")
	if err != nil {
		return report, fmt.Errorf("automaton safe-delete: list worktrees: %w: %s", err, strings.TrimSpace(wtOut))
	}
	worktrees, primaryBranch := parseWorktreeList(wtOut)

	// Candidate branches in for-each-ref order, with each branch's ahead
	// count resolved (per-branch rev-list count only on the fallback path).
	plans := []sdPlan{}
	for _, branch := range branchNames {
		if branch == "" || branch == base || branch == primaryBranch {
			continue
		}
		p := sdPlan{branch: branch, wt: worktrees[branch]}
		if fellBack {
			countOut, cerr := git.Run(ctx, baseRepo, "rev-list", "--count", base+".."+branch)
			if cerr != nil {
				p.err = fmt.Errorf("automaton merge compare: rev-list: %v: %s", cerr, strings.TrimSpace(countOut))
			} else {
				n, convErr := strconv.Atoi(strings.TrimSpace(countOut))
				if convErr != nil {
					p.err = fmt.Errorf("automaton merge compare: rev-list count: %v: %s", convErr, strings.TrimSpace(countOut))
				} else {
					p.ahead = n
				}
			}
		} else {
			p.ahead = aheads[branch]
		}
		plans = append(plans, p)
	}

	// Read the base subject list once per report — and only when at least
	// one branch is ahead, since an empty branch needs no further calls.
	var baseErr string
	var baseSubjects map[string]struct{}
	anyAhead := false
	for _, p := range plans {
		if p.ahead > 0 {
			anyAhead = true
			break
		}
	}
	if anyAhead {
		baseOut, berr := git.Run(ctx, baseRepo, "log", base, "--format=%s")
		if berr != nil {
			baseErr = fmt.Sprintf("Automaton merge compare: log base subjects: %v: %s", berr, strings.TrimSpace(baseOut))
		} else {
			baseSubjects = make(map[string]struct{})
			for _, s := range strings.Split(baseOut, "\n") {
				baseSubjects[s] = struct{}{}
			}
		}
	}

	// Per-branch work through a bounded worker pool; results keep the
	// for-each-ref (alphabetical) order.
	results := make([]sdResult, len(plans))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < safeDeleteGitConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = sdBranchWork(ctx, git, baseRepo, base, plans[i], opts, baseSubjects, baseErr)
			}
		}()
	}
	for i := range plans {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, p := range plans {
		r := results[i]
		switch r.cmp.State {
		case MergeStateMerged, MergeStateEmpty:
			reason := safeReasonMerged(base)
			if r.cmp.State == MergeStateEmpty {
				reason = safeReasonEmpty(base)
			}
			if p.wt != "" && !r.skipped {
				if r.statusErr != nil {
					report.Held = append(report.Held, SafeDeleteHeld{
						Branch: p.branch, Worktree: p.wt, State: MergeStateUnknown,
						Note: fmt.Sprintf("automaton safe-delete: worktree status: %v: %s", r.statusErr, strings.TrimSpace(r.statusOut)),
					})
					continue
				}
				if r.dirty {
					report.Held = append(report.Held, SafeDeleteHeld{
						Branch: p.branch, Worktree: p.wt, State: r.cmp.State, Dirty: true,
						Note: "Automaton worktree has uncommitted changes",
					})
					report.HeldDirty++
					continue
				}
			}
			entry := SafeDeleteEntry{
				Branch: p.branch, Worktree: p.wt, Reason: reason, Commits: r.cmp.Ahead,
			}
			if r.skipped {
				entry.DirtyCheckSkipped = true
				entry.Reason += "; dirty check skipped because the Automaton is running"
			}
			report.Safe = append(report.Safe, entry)
		default:
			held := SafeDeleteHeld{Branch: p.branch, Worktree: p.wt, State: r.cmp.State}
			if r.cmp.Error != "" {
				held.Note = r.cmp.Error
			}
			report.Held = append(report.Held, held)
		}
	}
	return report, nil
}

// sdBranchWork does one candidate branch's git work for the safe-delete
// report. It makes at most two calls (cherry, branch subject log) and a
// status check only for safe branches that own a worktree (skipped when the
// branch's Automaton is running); a branch with zero commits ahead makes
// none of the classification calls.
func sdBranchWork(ctx context.Context, git MergeGitRunner, repo, base string, p sdPlan, opts SafeDeleteOptions, baseSubjects map[string]struct{}, baseErr string) sdResult {
	if p.err != nil {
		return sdResult{cmp: MergeCompare{State: MergeStateUnknown, Error: p.err.Error()}}
	}
	var cmp MergeCompare
	switch {
	case p.ahead == 0:
		cmp = MergeCompare{Ahead: 0, State: MergeStateEmpty}
	case baseErr != "":
		cmp = MergeCompare{Ahead: p.ahead, State: MergeStateUnknown, Error: baseErr}
	default:
		cmp = sdCompareBranchAhead(ctx, git, repo, p.branch, base, p.ahead, baseSubjects)
	}
	r := sdResult{cmp: cmp}
	if p.wt != "" && (cmp.State == MergeStateMerged || cmp.State == MergeStateEmpty) {
		// A running Automaton's worktree is expected to churn, so the
		// report does not read its status; the entry stays in the safe
		// list with its classification unchanged. The report is
		// informational only: the worktree cleanup still re-checks for
		// uncommitted work at removal time.
		if opts.IsRunning != nil && opts.IsRunning(p.branch) {
			return sdResult{cmp: cmp, skipped: true}
		}
		status, serr := git.Run(ctx, repo, "-C", p.wt, "status", "--porcelain")
		if serr != nil {
			r.statusErr = serr
			r.statusOut = status
		} else {
			// Same daemon-section rule as the cleanup core: a tracked file
		// whose only difference from HEAD is the daemon's own marked
		// sections is not uncommitted work (the daemon rewrites that block
		// at the next session start), so it does not hold the branch dirty.
			status = filterDaemonOnlyPorcelain(ctx, p.wt, status, func(ctx context.Context, dir string, args ...string) (string, error) {
				runnerArgs := append([]string{"-C", dir}, args...)
				return git.Run(ctx, repo, runnerArgs...)
			})
			r.dirty = strings.TrimSpace(status) != ""
		}
	}
	return r
}

// sdCompareBranchAhead classifies an ahead branch using at most two git
// calls (cherry, branch subject log); the base subject set is shared once
// across the report and the shared precomputed-classification helper makes
// the decision.
func sdCompareBranchAhead(ctx context.Context, git MergeGitRunner, repo, branch, base string, ahead int, baseSubjects map[string]struct{}) MergeCompare {
	res := MergeCompare{State: MergeStateUnknown, Ahead: ahead}
	fail := func(what string, out string, err error) MergeCompare {
		res.State = MergeStateUnknown
		res.Error = fmt.Sprintf("Automaton merge compare: %s: %v: %s", what, err, strings.TrimSpace(out))
		return res
	}

	cherryOut, err := git.Run(ctx, repo, "cherry", base, branch)
	if err != nil {
		return fail("cherry", cherryOut, err)
	}
	subjects, err := git.Run(ctx, repo, "log", base+".."+branch, "--format=%s")
	if err != nil {
		return fail("log branch subjects", subjects, err)
	}
	classifyMergeFromPrecomputed(&res, ahead, cherryOut, subjects, baseSubjects)
	return res
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
