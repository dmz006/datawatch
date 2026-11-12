// Protected-paths gate: the check that runs once per worker attempt, right
// after the verification result is stored and before the file-drift checks.
//
// checkProtectedPaths asks one question: did this task change a protected
// path? It answers from git alone (read-only, no LLM) by comparing the task
// directory's working tree with the base commit, so it sees committed changes,
// uncommitted edits to tracked files (the daemon's own injection into the
// rules file is one) and new untracked files. Unlike the file-drift checks it
// is not skipped for worktree-mode Automata or for an empty PreTaskSHA: the
// base commit comes from resolveBaseCommit, which falls back to the merge-base
// with the Automaton base branch.
//
// What happens to a hit is decided by autonomous.automation.protected_paths_action
// (hold, warn, block, off); the record, the operator question and the alerts
// live in protected_paths_hold.go. The gate never reverts, resets, amends or
// rewrites anything.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// protectedPathsCheckSkippedDecisionKind is the Decision the gate records when
// it could not run the check, so a skipped check is never mistaken for a pass.
const protectedPathsCheckSkippedDecisionKind = "protected_paths_check_skipped"

// checkProtectedPaths runs the protected-paths gate for one worker attempt of
// t, whatever the verifier said, in direct mode and in worktree mode.
//
// It returns held=true when the task was held (parked in TaskBlocked with a
// hold record and one operator question); the caller persists nothing further
// and returns nil, as for the uncommitted-work hold. For the block action the
// task is parked in TaskBlocked with no question and held is false: the caller
// sees the blocked status, saves and returns, as for a file-drift block. For
// warn the finding is recorded and the task continues. A hold that cannot be
// recorded is returned wrapped.
//
// The check is skipped (and the task continues) when the action is off, when
// the task has no directory, and, after recording a
// protected_paths_check_skipped Decision and raising a warn alert, when the
// base commit or the changed set cannot be read.
func (m *Manager) checkProtectedPaths(ctx context.Context, prd *PRD, t *Task) (held bool, err error) {
	action := m.Config().Automation.EffectiveProtectedPathsAction()
	if action == config.ProtectedPathsActionOff {
		return false, nil
	}
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" {
		log.Printf("[autonomous] %s/%s protected-paths: skipped, no task directory", prd.ID, t.ID)
		return false, nil
	}
	rulesFile, _ := m.rulesAndContextFiles(dir)
	patterns := m.Config().Automation.EffectiveProtectedPaths(rulesFile)

	base, _, berr := m.resolveBaseCommit(ctx, prd, t)
	if berr != nil {
		if ctx.Err() != nil {
			log.Printf("[autonomous] %s/%s protected-paths: stopped: %v", prd.ID, t.ID, ctx.Err())
			return false, nil
		}
		m.recordProtectedPathsCheckSkipped(prd, t, "the base commit could not be resolved: "+protectedPathsCause(berr))
		return false, nil
	}

	changed, cerr := m.protectedPathsChangedSet(ctx, dir, base, t)
	if cerr != nil {
		if ctx.Err() != nil {
			log.Printf("[autonomous] %s/%s protected-paths: stopped: %v", prd.ID, t.ID, ctx.Err())
			return false, nil
		}
		m.recordProtectedPathsCheckSkipped(prd, t, "the changed files could not be read: "+protectedPathsCause(cerr))
		return false, nil
	}

	hits := MatchProtectedPaths(changed, patterns)
	if len(hits) == 0 {
		return false, nil
	}

	// A hit whose only change against base is the daemon's own sections (its
	// rewrites of the rules file) is not a change the task made.
	hits = m.withoutDaemonOnlyChanges(ctx, dir, base, hits)
	if len(hits) == 0 {
		return false, nil
	}

	var approved []string
	var existingHold *ProtectedPathHold
	m.store.WithLock(func() {
		approved = append(approved, t.ApprovedProtectedPaths...)
		if t.ProtectedPathHold != nil {
			h := *t.ProtectedPathHold
			existingHold = &h
		}
	})
	hits = withoutPaths(hits, approved)
	if len(hits) == 0 {
		return false, nil
	}

	needs, allowed := SplitAuthorized(hits, prd.AuthorizedProtectedPaths)
	if len(allowed) > 0 {
		m.recordProtectedPathsAuthorised(prd, t, allowed)
	}
	if len(needs) == 0 {
		return false, nil
	}

	switch action {
	case config.ProtectedPathsActionWarn:
		m.warnProtectedPaths(prd, t, needs, base)
		return false, nil
	case config.ProtectedPathsActionBlock:
		if berr := m.blockProtectedPaths(prd, t, needs, base); berr != nil {
			return false, fmt.Errorf("block task %s for protected paths: %w", t.ID, berr)
		}
		return false, nil
	}

	// hold: a task that already carries an open hold for exactly these paths
	// raises nothing more.
	if existingHold != nil && sameStringSet(existingHold.Paths, needs) && m.openUncommittedQuestion(existingHold.QuestionID) {
		log.Printf("[autonomous] %s/%s protected-paths: already held for these paths", prd.ID, t.ID)
		return true, nil
	}
	if herr := m.holdForProtectedPaths(prd, t, needs, base); herr != nil {
		return true, fmt.Errorf("hold task %s for protected paths: %w", t.ID, herr)
	}
	return true, nil
}

// protectedPathsChangedSet returns the sorted paths the task changed since
// base in dir: the union of the working tree against base (committed changes
// and uncommitted edits to tracked files, a rename or delete counting for both
// paths) and the new untracked files. A path that was already dirty or
// untracked before the task started is the operator's own work and is left
// out unless a commit made since base touched it.
func (m *Manager) protectedPathsChangedSet(ctx context.Context, dir, base string, t *Task) ([]string, error) {
	run := m.baseCommitRunner()
	diffOut, err := run(ctx, dir, "diff", "--name-only", "--no-renames", base)
	if err != nil {
		return nil, fmt.Errorf("git diff against %s: %w", shortSHA(base), err)
	}
	untrackedOut, err := run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	changed := sortedUnion(gitPathLines(diffOut), gitPathLines(untrackedOut))

	var preDirty, preUntracked []string
	m.store.WithLock(func() {
		preDirty = append(preDirty, t.PreTaskDirtyFiles...)
		preUntracked = append(preUntracked, t.PreTaskUntrackedFiles...)
	})
	pre := map[string]bool{}
	for _, f := range append(preDirty, preUntracked...) {
		if n := normalizeDriftPath(f); n != "" {
			pre[n] = true
		}
	}
	touchesPre := false
	for _, c := range changed {
		if pre[normalizeDriftPath(c)] {
			touchesPre = true
			break
		}
	}
	if !touchesPre {
		return changed, nil
	}

	committed, err := CommittedFilesSince(ctx, dir, base, run)
	if err != nil {
		return nil, fmt.Errorf("git log since %s: %w", shortSHA(base), err)
	}
	inCommit := map[string]bool{}
	for _, c := range committed {
		inCommit[normalizeDriftPath(c)] = true
	}
	var out []string
	for _, c := range changed {
		n := normalizeDriftPath(c)
		if pre[n] && !inCommit[n] {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// gitPathLines splits git's newline-separated path output, undoing git's
// C-style quoting of unusual names.
func gitPathLines(out string) []string {
	lines := splitLines(out)
	for i, l := range lines {
		if len(l) >= 2 && l[0] == '"' && l[len(l)-1] == '"' {
			if u, err := strconv.Unquote(l); err == nil {
				lines[i] = u
			}
		}
	}
	return lines
}

// withoutPaths returns paths minus the ones in remove, compared after
// normalisation.
func withoutPaths(paths, remove []string) []string {
	if len(remove) == 0 {
		return paths
	}
	drop := map[string]bool{}
	for _, r := range remove {
		if n := normalizeDriftPath(r); n != "" {
			drop[n] = true
		}
	}
	var out []string
	for _, p := range paths {
		if !drop[normalizeDriftPath(p)] {
			out = append(out, p)
		}
	}
	return out
}

// sameStringSet reports whether a and b hold the same strings, ignoring order
// and repeats.
func sameStringSet(a, b []string) bool {
	x, y := sortedUnion(a), sortedUnion(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// protectedPathsCause is a one-line, bounded reason taken from err.
func protectedPathsCause(err error) string {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(msg); len(r) > uncommittedCauseMaxRunes {
		msg = string(r[:uncommittedCauseMaxRunes]) + "…"
	}
	return msg
}

// recordProtectedPathsCheckSkipped logs that the check could not run, records
// a protected_paths_check_skipped Decision and raises a warn alert (nil-safe),
// so a check that did not run is never read as a pass. The task continues.
func (m *Manager) recordProtectedPathsCheckSkipped(prd *PRD, t *Task, reason string) {
	log.Printf("[autonomous] WARNING %s/%s protected-paths: check skipped: %s", prd.ID, t.ID, reason)
	m.appendUncommittedDecision(prd, protectedPathsCheckSkippedDecisionKind, "task "+t.ID+": protected paths check skipped: "+reason, "")
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save Automaton: %v", prd.ID, t.ID, err)
	}
	m.mu.Lock()
	alertFn := m.alertSink
	m.mu.Unlock()
	if alertFn != nil {
		alertFn("warn", "Protected paths check skipped",
			"Automaton "+prd.ID+" ("+prd.Title+"), task "+t.ID+" ("+t.Title+"): the protected paths check did not run: "+reason)
	}
}

// recordProtectedPathsAuthorised records, once per set of paths, that the
// Automaton's authorised paths let these protected paths through.
func (m *Manager) recordProtectedPathsAuthorised(prd *PRD, t *Task, allowed []string) {
	note := "task " + t.ID + ": protected paths changed under the Automaton's authorised paths: " +
		boundedFileList(allowed, protectedPathsQuestionFiles)
	seen := false
	m.store.WithLock(func() {
		for _, d := range prd.Decisions {
			if d.Kind == protectedPathsAuthorisedDecisionKind && d.Note == note {
				seen = true
				return
			}
		}
	})
	if seen {
		return
	}
	m.appendUncommittedDecision(prd, protectedPathsAuthorisedDecisionKind, note, "")
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save Automaton: %v", prd.ID, t.ID, err)
	}
}

// protectedPathsBlockedTask reports whether the gate's block action parked t:
// the executor stops the walk for such a task after saving it.
func (m *Manager) protectedPathsBlockedTask(t *Task) bool {
	blocked := false
	m.store.WithLock(func() {
		blocked = t.Status == TaskBlocked && strings.HasPrefix(t.Error, "protected_path_blocked: ")
	})
	return blocked
}
