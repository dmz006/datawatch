// Merging a verified task branch into the PRD branch. With worktree_per_task
// on, a task works on its own branch (task_worktree.go). Once it passes
// verification its branch is folded into the PRD branch, in dependency order,
// under a short per-PRD lock so two merges never touch the PRD worktree at
// once. A conflict is settled by autonomous.parallel.merge_conflict_policy:
//
//	additive_then_model  purely additive conflicts are resolved mechanically,
//	                     the rest by the configured model, accepted only when
//	                     the verification passes; otherwise the task is held
//	                     and the operator is asked through the held-question
//	                     path
//	block                the Automaton is held for the operator
//	triage               the task and Automaton go to the triage path
//
// Nothing is forced or discarded: an unfinished merge is only ever backed out
// with "git merge --abort", which restores the tree the merge started from.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Task merge states (Task.MergeState). "pending" and "merged" are the
// MergeState constants merge_compare.go declares; these are the other two.
const (
	MergeStateConflict = "conflict"
	MergeStateHeld     = "held"
)

// MergeOutcome is the result of one MergeTaskBranch call.
type MergeOutcome string

const (
	// MergeOutcomeSkipped: the task has no branch to merge or is not completed.
	MergeOutcomeSkipped MergeOutcome = "skipped"
	// MergeOutcomeDeferred: a dependency or an earlier sibling is not merged yet.
	MergeOutcomeDeferred MergeOutcome = "deferred"
	// MergeOutcomeMerged: the branch is on the PRD branch.
	MergeOutcomeMerged MergeOutcome = "merged"
	// MergeOutcomeConflict: a conflict the policy sent to block or triage.
	MergeOutcomeConflict MergeOutcome = "conflict"
	// MergeOutcomeHeld: a conflict nothing could settle; the task is held.
	MergeOutcomeHeld MergeOutcome = "held"
)

// MergeConflictFile is one file a merge left conflicted.
type MergeConflictFile struct {
	Path       string // path relative to the PRD worktree
	Base       string // common ancestor content ("" when the file had none)
	Ours       string // PRD branch content
	Theirs     string // task branch content
	Conflicted string // working tree content with conflict markers
}

// MergeResolveRequest asks the model to resolve the conflicted files.
type MergeResolveRequest struct {
	PRD   *PRD
	Task  *Task
	Model string // merge_conflict_model; empty = strongest planning model
	Dir   string // the PRD worktree the merge runs in
	Files []MergeConflictFile
}

// MergeResolveFn returns the resolved content of every file it was given,
// keyed by path.
type MergeResolveFn func(ctx context.Context, req MergeResolveRequest) (map[string]string, error)

// MergeVerifyFn runs the verification/tests against the merged tree in dir,
// before the merge is committed. A nil error means they pass.
type MergeVerifyFn func(ctx context.Context, prd *PRD, task *Task, dir string) error

// SetMergeResolver wires the model that resolves non-additive conflicts and
// the verification that must pass before its answer is accepted.
func (m *Manager) SetMergeResolver(resolve MergeResolveFn, verify MergeVerifyFn) {
	m.mu.Lock()
	m.mergeResolveFn, m.mergeVerifyFn = resolve, verify
	m.mu.Unlock()
}

// prdMergeLocks holds one mutex per PRD: the short merge lock.
var prdMergeLocks sync.Map

func prdMergeLock(prdID string) *sync.Mutex {
	l, _ := prdMergeLocks.LoadOrStore(prdID, &sync.Mutex{})
	return l.(*sync.Mutex)
}

// mergeBlocker says why t cannot merge yet: a dependency that works on a
// branch is not merged, or an earlier task in task order that is completed
// and waiting to merge has not. Empty when t may merge now.
func mergeBlocker(prd *PRD, t *Task) string {
	byID := map[string]*Task{}
	var order []*Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			x := &prd.Story[si].Tasks[ti]
			byID[x.ID] = x
			order = append(order, x)
		}
	}
	for _, id := range t.DependsOn {
		d := byID[id]
		if d == nil || d.WorktreeBranch == "" {
			continue // no branch of its own: nothing to wait for
		}
		if d.MergeState != MergeStateMerged {
			return fmt.Sprintf("dependency %s is not merged yet", d.ID)
		}
	}
	for _, x := range order {
		if x == t {
			break
		}
		if x.WorktreeBranch != "" && x.Status == TaskCompleted &&
			(x.MergeState == "" || x.MergeState == MergeStatePending) {
			return fmt.Sprintf("earlier task %s is waiting to merge", x.ID)
		}
	}
	return ""
}

// MergeCompletedTasks merges every completed task of prd that is waiting to
// merge, in task order, repeating while merges make progress (a dependency
// declared after its dependent needs a second pass). Returns the merged count.
func (m *Manager) MergeCompletedTasks(ctx context.Context, prd *PRD) (int, error) {
	if prd == nil {
		return 0, nil
	}
	merged := 0
	var autoFixTask *Task
	for {
		progress := false
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.WorktreeBranch == "" || t.Status != TaskCompleted ||
					(t.MergeState != "" && t.MergeState != MergeStatePending) {
					continue
				}
				out, err := m.MergeTaskBranch(ctx, prd, t)
				if err != nil {
					return merged, err
				}
				if out == MergeOutcomeMerged {
					merged++
					progress = true
					if isAutoFixTask(t) {
						autoFixTask = t
					}
				}
			}
		}
		if !progress {
			// Auto re-review. An auto-fix's work just landed on the Automaton
			// branch, moving its head past the completion verdict any block was
			// made on. Run the completion review once for the new head and, if
			// it passes, clear the stale block — automatically, with no operator
			// in the loop (see AutoRejudgeAfterAutoFixMerge, rejudge_autofix.go).
			// It fires at most once per head, so a review that already answered
			// this head is a no-op. The merge lock is released before this runs
			// and the re-judgment takes no merge lock.
			if autoFixTask != nil {
				m.AutoRejudgeAfterAutoFixMerge(ctx, prd, autoFixTask)
			}
			return merged, nil
		}
	}
}

// MergeTaskBranch merges the verified task t's branch into the PRD branch.
// A task without a branch is skipped; a task whose dependencies (or earlier
// waiting siblings) are not merged is deferred and marked pending.
func (m *Manager) MergeTaskBranch(ctx context.Context, prd *PRD, t *Task) (MergeOutcome, error) {
	if prd == nil || t == nil || t.WorktreeBranch == "" || prd.ProjectDir == "" {
		return MergeOutcomeSkipped, nil
	}
	if t.MergeState == MergeStateMerged {
		return MergeOutcomeMerged, nil
	}
	if t.Status != TaskCompleted {
		return MergeOutcomeSkipped, nil
	}

	lock := prdMergeLock(prd.ID)
	lock.Lock()
	defer lock.Unlock()

	if why := mergeBlocker(prd, t); why != "" {
		m.setMergeState(t, MergeStatePending, "")
		return MergeOutcomeDeferred, nil
	}

	dir := prd.ProjectDir
	if taskBranchMerged(ctx, dir, t.WorktreeBranch, "HEAD") {
		sha := strings.TrimSpace(gitText(ctx, dir, "rev-parse", "HEAD"))
		m.setMergeState(t, MergeStateMerged, sha)
		return MergeOutcomeMerged, nil
	}
	if blk := dirtyWorktreeBlockWith(ctx, dir, m.baseCommitRunner()); blk != nil {
		first := t.MergeBlock == nil || t.MergeBlock.Kind != blk.Kind
		blk.LastRetryAt = time.Now()
		if err := m.setMergeBlock(t, *blk); err != nil {
			return MergeOutcomeDeferred, err
		}
		m.setMergeState(t, MergeStatePending, "")
		if first {
			m.alertMergeBlocked(t, blk)
		}
		return MergeOutcomeDeferred, nil
	}
	if err := m.clearMergeBlock(t); err != nil {
		return MergeOutcomeDeferred, err
	}

	if cerr := m.cleanTaskBranchDaemonSections(ctx, prd, t); cerr != nil {
		return MergeOutcomeDeferred, fmt.Errorf("task merge: %w", cerr)
	}

	msg := fmt.Sprintf("Merge task %s: %s", t.ID, t.Title)
	args := append(gitIdentityArgs(ctx, dir), "merge", "--no-ff", "-m", msg, t.WorktreeBranch)
	out, mergeErr := runGitInCtx(ctx, dir, args...)
	if mergeErr == nil {
		sha := strings.TrimSpace(gitText(ctx, dir, "rev-parse", "HEAD"))
		m.setMergeState(t, MergeStateMerged, sha)
		return MergeOutcomeMerged, nil
	}
	conflicts := unmergedPaths(ctx, dir)
	if len(conflicts) == 0 {
		abortMerge(ctx, dir)
		return MergeOutcomeDeferred, fmt.Errorf("task merge %s into PRD branch: %s: %w", t.WorktreeBranch, strings.TrimSpace(out), mergeErr)
	}

	policy := EffectiveMergeConflictPolicy(m.Config().Parallel)
	summary := fmt.Sprintf("merging task branch %s into the PRD branch conflicts in: %s", t.WorktreeBranch, strings.Join(conflicts, ", "))
	switch policy {
	case config.MergeConflictPolicyBlock:
		abortMerge(ctx, dir)
		m.recordMergeConflict(prd, t, summary, PRDBlocked)
		return MergeOutcomeConflict, nil
	case config.MergeConflictPolicyTriage:
		abortMerge(ctx, dir)
		m.recordMergeConflict(prd, t, summary, PRDFailed)
		return MergeOutcomeConflict, nil
	}

	// additive_then_model.
	why, ok := m.resolveConflicts(ctx, prd, t, dir, conflicts)
	if ok {
		if _, cerr := runGitInCtx(ctx, dir, append(gitIdentityArgs(ctx, dir), "commit", "--no-edit")...); cerr != nil {
			abortMerge(ctx, dir)
			return MergeOutcomeDeferred, fmt.Errorf("task merge: commit resolved merge: %w", cerr)
		}
		sha := strings.TrimSpace(gitText(ctx, dir, "rev-parse", "HEAD"))
		m.setMergeState(t, MergeStateMerged, sha)
		return MergeOutcomeMerged, nil
	}
	abortMerge(ctx, dir)
	m.holdMergeTask(prd, t, summary+"; "+why)
	return MergeOutcomeHeld, nil
}

// cleanTaskBranchDaemonSections strips the daemon's own sections from the
// rules file that t's branch committed, in one cleanup commit on that branch
// (daemon_sections_merge.go), so the merge never folds them into the PRD
// branch. It is a no-op with a log line when the task has no worktree of its
// own (nothing to put the cleanup commit on). An error means the merge must
// not run.
func (m *Manager) cleanTaskBranchDaemonSections(ctx context.Context, prd *PRD, t *Task) error {
	tw := strings.TrimSpace(t.WorktreePath)
	if tw == "" {
		log.Printf("[autonomous] %s task %s: no task worktree; skipping the pre-merge daemon-section cleanup", prd.ID, t.ID)
		return nil
	}
	base := ""
	for _, sha := range []string{t.BaseSHA, t.PreTaskSHA} {
		if sha = strings.TrimSpace(sha); sha != "" {
			base = sha
			break
		}
	}
	if base == "" {
		base = strings.TrimSpace(gitText(ctx, prd.ProjectDir, "merge-base", t.WorktreeBranch, "HEAD"))
	}
	if base == "" {
		return fmt.Errorf("no base commit to strip the daemon sections of task %s's rules file against", t.ID)
	}
	if err := m.cleanDaemonSectionsBeforeMerge(ctx, tw, base); err != nil {
		log.Printf("[autonomous] %s task %s: pre-merge daemon-section cleanup in %s failed: %v", prd.ID, t.ID, tw, err)
		return err
	}
	log.Printf("[autonomous] %s task %s: pre-merge daemon-section cleanup in %s done (base %s)", prd.ID, t.ID, tw, base)
	return nil
}

// resolveConflicts settles every conflicted path in the open merge: purely
// additive ones mechanically, the rest through the model, accepted only when
// the verification passes. It reports why it could not when ok is false.
func (m *Manager) resolveConflicts(ctx context.Context, prd *PRD, t *Task, dir string, paths []string) (why string, ok bool) {
	var rest []string
	for _, p := range paths {
		if resolved, err := resolveAdditive(ctx, dir, p); err == nil && resolved {
			continue
		}
		rest = append(rest, p)
	}
	if len(rest) == 0 {
		return "", true
	}

	m.mu.Lock()
	resolve, verify := m.mergeResolveFn, m.mergeVerifyFn
	m.mu.Unlock()
	if resolve == nil || verify == nil {
		return fmt.Sprintf("%d file(s) conflict in ways that are not purely additive and no model resolution is configured: %s", len(rest), strings.Join(rest, ", ")), false
	}

	files := make([]MergeConflictFile, 0, len(rest))
	for _, p := range rest {
		f := MergeConflictFile{Path: p}
		f.Base = gitText(ctx, dir, "show", ":1:"+p)
		f.Ours = gitText(ctx, dir, "show", ":2:"+p)
		f.Theirs = gitText(ctx, dir, "show", ":3:"+p)
		if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil { // #nosec G304 -- p comes from git's own unmerged list
			f.Conflicted = string(b)
		}
		files = append(files, f)
	}
	answer, err := resolve(ctx, MergeResolveRequest{
		PRD: prd, Task: t, Dir: dir, Files: files,
		Model: EffectiveMergeConflictModel(m.Config().Parallel),
	})
	if err != nil {
		return "the model could not resolve the conflict: " + err.Error(), false
	}
	for _, p := range rest {
		content, found := answer[p]
		if !found || strings.Contains(content, "<<<<<<<") && strings.Contains(content, ">>>>>>>") {
			return fmt.Sprintf("the model left %s unresolved", p), false
		}
		full, perr := safeJoin(dir, p)
		if perr != nil {
			return perr.Error(), false
		}
		if werr := os.WriteFile(full, []byte(content), 0o644); werr != nil { // #nosec G306 -- source file in a worktree
			return "write resolution: " + werr.Error(), false
		}
		if _, aerr := runGitInCtx(ctx, dir, "add", "--", p); aerr != nil {
			return "stage resolution: " + aerr.Error(), false
		}
	}
	if verr := verify(ctx, prd, t, dir); verr != nil {
		return "the model's resolution failed the verification: " + verr.Error(), false
	}
	return "", true
}

// resolveAdditive resolves one conflicted file when both sides only added
// lines to the common ancestor, by keeping both additions. Returns false (and
// leaves the file untouched) for anything else: a deletion, a binary file, or
// a file one side removed or created.
func resolveAdditive(ctx context.Context, dir, path string) (bool, error) {
	base, errB := runGitInCtx(ctx, dir, "show", ":1:"+path)
	ours, errO := runGitInCtx(ctx, dir, "show", ":2:"+path)
	theirs, errT := runGitInCtx(ctx, dir, "show", ":3:"+path)
	if errB != nil || errO != nil || errT != nil {
		return false, nil
	}
	tmp, err := os.MkdirTemp("", "merge-additive-")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	write := func(name, body string) string {
		p := filepath.Join(tmp, name)
		_ = os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	fb, fo, ft := write("base", base), write("ours", ours), write("theirs", theirs)
	for _, side := range []string{fo, ft} {
		if !onlyAdditions(ctx, tmp, fb, side) {
			return false, nil
		}
	}
	merged, err := runGitInCtx(ctx, tmp, "merge-file", "-p", "--union", fo, fb, ft)
	if err != nil {
		return false, err
	}
	full, err := safeJoin(dir, path)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(full, []byte(merged), 0o644); err != nil { // #nosec G306 -- source file in a worktree
		return false, err
	}
	if _, err := runGitInCtx(ctx, dir, "add", "--", path); err != nil {
		return false, err
	}
	return true, nil
}

// onlyAdditions reports whether side differs from base by added lines only.
func onlyAdditions(ctx context.Context, dir, base, side string) bool {
	// --no-index exits 1 when the files differ, so the error is expected.
	out, _ := runGitInCtx(ctx, dir, "diff", "--no-index", "--numstat", base, side)
	out = strings.TrimSpace(out)
	if out == "" {
		return true
	}
	fields := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if len(fields) < 2 {
		return false
	}
	del, err := strconv.Atoi(fields[1])
	return err == nil && del == 0 // "-" (binary) fails the parse
}

// safeJoin joins rel under dir and refuses a path that leaves it.
func safeJoin(dir, rel string) (string, error) {
	full := filepath.Join(dir, rel)
	r, err := filepath.Rel(dir, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("path %q escapes the worktree", rel)
	}
	return full, nil
}

// unmergedPaths lists the files an open merge left conflicted.
func unmergedPaths(ctx context.Context, dir string) []string {
	var out []string
	for _, l := range strings.Split(gitText(ctx, dir, "diff", "--name-only", "--diff-filter=U"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// abortMerge backs out an unfinished merge, restoring the tree it began from.
func abortMerge(ctx context.Context, dir string) {
	if _, err := runGitInCtx(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err != nil {
		return // no merge in progress
	}
	if _, err := runGitInCtx(ctx, dir, "merge", "--abort"); err != nil {
		log.Printf("[autonomous] task merge: abort in %s: %v", dir, err)
	}
}

// gitText returns a git command's output, "" on error.
func gitText(ctx context.Context, dir string, args ...string) string {
	out, err := runGitInCtx(ctx, dir, args...)
	if err != nil {
		return ""
	}
	return out
}

// gitIdentityArgs supplies a committer identity only when none is configured,
// so a merge commit never fails on a host with no git identity.
func gitIdentityArgs(ctx context.Context, dir string) []string {
	if strings.TrimSpace(gitText(ctx, dir, "config", "user.email")) != "" &&
		strings.TrimSpace(gitText(ctx, dir, "config", "user.name")) != "" {
		return nil
	}
	return []string{"-c", "user.name=datawatch", "-c", "user.email=datawatch@localhost"}
}

// setMergeState records the merge state (and commit) on the task.
func (m *Manager) setMergeState(t *Task, state, commit string) {
	m.store.WithLock(func() {
		t.MergeState = state
		if commit != "" {
			t.MergeCommit = commit
		}
	})
	_ = m.store.SaveTask(t)
}

// mergeFailure is the structured reason a merge conflict leaves on a task.
func mergeFailure(detail string) *FailureReason {
	return &FailureReason{
		Category:   FailureCategoryContent,
		Detail:     detail,
		Source:     "task-merge",
		OccurredAt: time.Now(),
	}
}

// holdMergeTask holds t for the operator: it takes the review-held state the
// held-question pass already raises a question for.
func (m *Manager) holdMergeTask(prd *PRD, t *Task, detail string) {
	now := time.Now()
	m.store.WithLock(func() {
		t.MergeState = MergeStateHeld
		t.Status = TaskReviewNeeded
		t.Error = detail
		t.FailureReason = mergeFailure(detail)
		t.ReviewSummary = &ReviewSummary{HeldAt: now, Text: detail}
	})
	_ = m.store.SaveTask(t)
}

// recordMergeConflict applies the block and triage policies: the task keeps
// its conflict state and the Automaton moves to prdStatus (blocked for the
// operator, or failed for the triage path, which also gets its record).
func (m *Manager) recordMergeConflict(prd *PRD, t *Task, detail string, prdStatus PRDStatus) {
	m.store.WithLock(func() {
		t.MergeState = MergeStateConflict
		t.Error = detail
		t.FailureReason = mergeFailure(detail)
		if prdStatus == PRDFailed {
			t.Status = TaskFailed
		}
		prd.Status = prdStatus
		if prdStatus == PRDBlocked {
			prd.BlockedReason = detail
		}
	})
	_ = m.store.SaveTask(t)
	if err := m.savePRDOwn(prd); err != nil {
		log.Printf("[autonomous] %s task merge: save PRD: %v", prd.ID, err)
	}
	if prdStatus == PRDFailed {
		if rec := m.BuildTriageRecordFor(prd); rec != nil {
			if err := m.SaveTriageRecord(prd.ID, rec); err != nil && !errors.Is(err, ErrTriageNoRecord) {
				log.Printf("[autonomous] %s task merge: triage record: %v", prd.ID, err)
			}
		}
	}
}
