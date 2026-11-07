// Operator answer to the protected-paths question.
//
// The hold code (protected_paths_hold.go) parks a task on a question with the
// options approve, reject and change_spec. This file acts on the answer. Only
// the question the hold itself names is acted on: a question whose id does not
// match the hold (the hold was cleared, replaced or already resolved) is stale
// and is recorded as a no-op that changes nothing on the task.
//
//	approve      adds the held paths to the task's approved paths and completes
//	             the task. The approval covers this task only; the Automaton's
//	             authorised paths are not touched.
//	reject       puts every listed path back to its state at the hold's base
//	             commit and commits that as a new commit (history is never
//	             rewritten), then runs the task again. No auto-fix retry budget
//	             is consumed.
//	change_spec  keeps the task blocked and marks the hold as awaiting a spec
//	             change. Editing the task and resetting it clears the hold, and
//	             the gate runs again on the next attempt.
//
// A failed restore keeps the hold, records the reason on it and raises a fresh
// question, so the operator is never left without a way to answer.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Kinds of Decision and audit event the protected-paths answer writes.
const (
	protectedPathsApprovedDecisionKind      = "protected_paths_approved"
	protectedPathsRejectedDecisionKind      = "protected_paths_rejected"
	protectedPathsSpecChangeDecisionKind    = "protected_paths_spec_change_requested"
	protectedPathsAnswerIgnoredDecisionKind = "protected_paths_answer_ignored"
	protectedPathsAnswerInvalidDecisionKind = "protected_paths_answer_invalid"

	protectedPathsAnsweredAuditAction = "autonomous_protected_paths_answered"
)

// protectedPathsAnswerTimeout bounds the git work one answer does.
const protectedPathsAnswerTimeout = 5 * time.Minute

// protectedPathsSpecChangeError is the task error while the hold waits for the
// operator to change the task instructions.
const protectedPathsSpecChangeError = "protected_path: change the task instructions, then reset the task"

// protectedPathsAnswer is one answered hold: the Automaton, the held task, a
// copy of the hold and who answered.
type protectedPathsAnswer struct {
	prd      *PRD
	t        *Task
	hold     ProtectedPathHold
	question Question
	actor    string
	option   string
	dir      string
}

// onProtectedPathsAnswered acts on the operator's answer to a protected-paths
// question (see the file comment) and then lets the Automaton run on, the way
// onUncommittedWorkAnswered does: a live executor is woken to re-scan and no
// second one is started; otherwise the run is relaunched.
func (a *API) onProtectedPathsAnswered(c QuestionChange) {
	prdID, changed := a.M.resolveProtectedPaths(c)
	if prdID == "" {
		return
	}
	a.M.EmitPRDUpdate(prdID)
	if !changed {
		return
	}
	if st, ok := a.prdStatus(prdID); !ok || st != PRDRunning {
		return
	}
	a.runMu.Lock()
	_, live := a.runCancels[prdID]
	if live {
		a.reviewWake.signal(prdID)
	}
	a.runMu.Unlock()
	if !live {
		if err := a.Run(prdID); err != nil {
			log.Printf("[autonomous] %s run after protected-paths answer: %v", prdID, err)
		}
	}
}

// resolveProtectedPaths acts on an answered protected-paths question. It
// returns the Automaton id the question belongs to ("" when the change is not
// one to act on) and whether the task was released, which is when the
// Automaton should run on. A question for another label, a change that is not
// an answer and the default "hold" option are ignored.
func (m *Manager) resolveProtectedPaths(c QuestionChange) (prdID string, resolved bool) {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || q.Label != ProtectedPathsQuestionLabel || c.Answer == nil {
		return "", false
	}
	if q.ID == "" || q.PRDID == "" || q.TaskID == "" {
		return "", false
	}
	option := c.Answer.ChosenOptionID
	if option == protectedPathsQuestionDefault {
		// The default keeps the hold as it is.
		return "", false
	}
	prd, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return "", false
	}
	t, _ := findTaskInPRD(prd, q.TaskID)
	if t == nil {
		return "", false
	}
	r := &protectedPathsAnswer{prd: prd, t: t, question: q, option: option, actor: c.Answer.Actor}
	if r.actor == "" {
		r.actor = "operator"
	}

	switch option {
	case ProtectedPathsOptionApprove, ProtectedPathsOptionReject, ProtectedPathsOptionChangeSpec:
	default:
		log.Printf("[autonomous] WARNING %s/%s protected-paths: unknown answer option %q", prd.ID, t.ID, option)
		m.appendProtectedPathsDecision(r, protectedPathsAnswerInvalidDecisionKind,
			fmt.Sprintf("task %s: unknown option %q; nothing changed", t.ID, option))
		return prd.ID, false
	}

	named := false
	m.store.WithLock(func() {
		h := t.ProtectedPathHold
		if h == nil || h.QuestionID != q.ID || t.Status != TaskBlocked {
			return
		}
		r.hold = *h
		r.hold.Paths = append([]string(nil), h.Paths...)
		named = true
	})
	if !named {
		log.Printf("[autonomous] %s/%s protected-paths: answer %q for a task that is no longer held", prd.ID, t.ID, option)
		m.appendProtectedPathsDecision(r, protectedPathsAnswerIgnoredDecisionKind,
			fmt.Sprintf("task %s: option=%s; the task is no longer held for this question; nothing changed", t.ID, option))
		return prd.ID, false
	}
	r.dir = m.resolveTaskProjectDir(prd, t)

	ctx, cancel := context.WithTimeout(context.Background(), protectedPathsAnswerTimeout)
	defer cancel()
	switch option {
	case ProtectedPathsOptionApprove:
		resolved = m.answerProtectedPathsApprove(ctx, r)
	case ProtectedPathsOptionReject:
		resolved = m.answerProtectedPathsReject(ctx, r)
	default:
		m.answerProtectedPathsChangeSpec(r)
	}
	return prd.ID, resolved
}

// answerProtectedPathsApprove approves the held paths for this task and
// completes it the way the commit answer completes a held uncommitted-work
// task: CompletedAt, the post-verify SHA from HEAD, TaskCompleted, and a
// blocked Automaton reopened so the next run rolls it up. The Automaton's
// authorised paths are not changed.
func (m *Manager) answerProtectedPathsApprove(ctx context.Context, r *protectedPathsAnswer) bool {
	prd, t := r.prd, r.t
	postSHA := CapturePostVerifySHA(ctx, r.dir, m.driftGit)
	done := time.Now()
	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.ApprovedProtectedPaths = sortedUnion(t.ApprovedProtectedPaths, r.hold.Paths)
		t.ProtectedPathHold = nil
		t.Error = ""
		if t.FailureReason != nil && strings.HasPrefix(t.FailureReason.Source, "protected-paths") {
			t.FailureReason = nil
		}
		t.CompletedAt = &done
		t.PostVerifySHA = postSHA
		t.Status = TaskCompleted
		t.UpdatedAt = done
	})
	m.appendProtectedPathsDecision(r, protectedPathsApprovedDecisionKind,
		"task "+t.ID+": approved protected paths: "+boundedFileList(r.hold.Paths, protectedPathsQuestionFiles))
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save task: %v", prd.ID, t.ID, err)
	}
	m.reopenAfterProtectedPathsAnswer(prd)
	m.auditProtectedPathsAnswer(r)
	m.emitTaskStatus(prd, t, old, lastDecision(prd, protectedPathsApprovedDecisionKind), nil)
	log.Printf("[autonomous] %s/%s protected-paths: approved %d path(s)", prd.ID, t.ID, len(r.hold.Paths))
	return true
}

// reopenAfterProtectedPathsAnswer returns a blocked Automaton to running and
// saves it, announcing the status change. Any other status is saved as it is.
func (m *Manager) reopenAfterProtectedPathsAnswer(prd *PRD) {
	var old PRDStatus
	changed := false
	m.store.WithLock(func() {
		old = prd.Status
		if prd.Status == PRDBlocked {
			prd.Status = PRDRunning
			prd.BlockedReason = ""
			changed = true
		}
		prd.UpdatedAt = time.Now()
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s protected-paths: save Automaton: %v", prd.ID, err)
	}
	if changed {
		m.emitPRDStatus(prd, old, lastDecision(prd, protectedPathsApprovedDecisionKind), nil)
	}
}

// protectedPathsRestoreMessage is the subject of the restore commit, in the
// style of the session's own task commits.
func protectedPathsRestoreMessage(prd *PRD) string {
	return "task(" + shortID(prd.ID) + "): restore protected paths"
}

// restoreCommitFn is the function the restore commit goes through: the
// injected task commit function when there is one. The default task commit
// stages each path with git add first, which git refuses for a path that the
// restore has just removed from the index, so with no injected function the
// restore commit is made with an explicit pathspec only, which records the
// deletion.
func (m *Manager) restoreCommitFn() TaskCommitFn {
	m.mu.Lock()
	custom := m.taskCommit
	m.mu.Unlock()
	if custom != nil {
		return custom
	}
	return restoreTaskCommit
}

// restoreTaskCommit commits exactly files (already staged by the restore) with
// message and returns the new HEAD sha; with nothing staged for them it makes
// no commit and returns the current HEAD.
func restoreTaskCommit(ctx context.Context, dir string, files []string, message string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("restore commit: empty directory")
	}
	if len(files) == 0 {
		return taskHeadSHA(ctx, dir)
	}
	args := append([]string{"diff", "--cached", "--quiet", "--"}, files...)
	if _, err := runGitInCtx(ctx, dir, args...); err == nil {
		return taskHeadSHA(ctx, dir)
	}
	args = append(gitIdentityArgs(ctx, dir), "commit", "-m", message, "--")
	args = append(args, files...)
	if out, err := runGitInCtx(ctx, dir, args...); err != nil {
		return "", fmt.Errorf("restore commit: git commit in %s: %w: %s", dir, err, strings.TrimSpace(out))
	}
	return taskHeadSHA(ctx, dir)
}

// answerProtectedPathsReject puts the held paths back to their state at the
// hold's base commit, commits that as a new commit and runs the task again. It
// reports whether the task was released.
func (m *Manager) answerProtectedPathsReject(ctx context.Context, r *protectedPathsAnswer) bool {
	prd, t := r.prd, r.t
	base := r.hold.BaseCommit
	if r.dir == "" {
		return m.failProtectedPathsReject(r, "the paths were not restored: the task has no directory")
	}
	if base == "" {
		return m.failProtectedPathsReject(r, "the paths were not restored: the hold has no base commit")
	}
	paths := sortedProtectedPaths(r.hold.Paths)
	for _, p := range paths {
		// Nothing is touched when any listed path is not inside the task
		// directory.
		if _, err := resolveInsideDir(r.dir, p); err != nil {
			return m.failProtectedPathsReject(r, "the paths were not restored: "+err.Error())
		}
	}

	run := m.baseCommitRunner()
	// toCommit lists the paths the restore leaves staged: the ones put back
	// from the base commit and the ones whose removal is a change against HEAD.
	var toCommit []string
	for _, p := range paths {
		inBase, err := pathInTree(ctx, run, r.dir, base, p)
		if err != nil {
			return m.failProtectedPathsReject(r, "the paths were not restored: "+protectedPathsCause(err))
		}
		if inBase {
			// The path existed at the base commit: put its content back.
			if _, err := run(ctx, r.dir, "checkout", base, "--", p); err != nil {
				return m.failProtectedPathsReject(r, "the paths were not restored: "+protectedPathsCause(err))
			}
			toCommit = append(toCommit, p)
			continue
		}
		// The task added the path: remove it from the index and the tree.
		inHead, err := pathInTree(ctx, run, r.dir, "HEAD", p)
		if err != nil {
			return m.failProtectedPathsReject(r, "the paths were not restored: "+protectedPathsCause(err))
		}
		if _, err := run(ctx, r.dir, "rm", "-q", "--ignore-unmatch", "--", p); err != nil {
			return m.failProtectedPathsReject(r, "the paths were not restored: "+protectedPathsCause(err))
		}
		// An added file that was never committed is not in the index, so git
		// leaves it; take it off disk as well. It is no change to commit.
		if full, perr := resolveInsideDir(r.dir, p); perr == nil {
			if rerr := os.Remove(full); rerr != nil && !os.IsNotExist(rerr) {
				return m.failProtectedPathsReject(r, "the paths were not restored: "+protectedPathsCause(rerr))
			}
		}
		if inHead {
			toCommit = append(toCommit, p)
		}
	}

	// A restored rules file whose content equals its HEAD content once the
	// daemon's own sections are stripped from both sides has nothing of the
	// task's to commit, so it stays out of the restore pathspec; when that
	// empties the list the commit is skipped, as for an empty list.
	toCommit = m.pathspecWithoutDaemonOnlyChanges(ctx, r.dir, toCommit)

	var sha string
	if len(toCommit) > 0 {
		var err error
		sha, err = m.restoreCommitFn()(ctx, r.dir, toCommit, protectedPathsRestoreMessage(prd))
		if err != nil {
			log.Printf("[autonomous] WARNING %s/%s protected-paths: restore commit failed: %v", prd.ID, t.ID, err)
			return m.failProtectedPathsReject(r, "the restore could not be committed: "+protectedPathsCause(err))
		}
	}

	// Back to pending through resetTask, the path reset_task and the review
	// retry use, so the attempt history is kept. It clears the hold and gives
	// back the task's retry count: the rejection consumes no auto-fix budget.
	if _, err := m.resetTask(prd.ID, t.ID, r.actor, false, false, nil); err != nil {
		log.Printf("[autonomous] WARNING %s/%s protected-paths: reset task: %v", prd.ID, t.ID, err)
		return m.failProtectedPathsReject(r, "the task could not be run again: "+protectedPathsCause(err))
	}
	list := boundedFileList(paths, protectedPathsQuestionFiles)
	m.store.WithLock(func() {
		t.ProtectedPathHold = nil
		hint := "The operator rejected the change to the protected paths " + list +
			" and they were put back to their state before this task. Do not change those paths."
		if t.PriorAttemptHint != "" {
			hint = t.PriorAttemptHint + " " + hint
		}
		t.PriorAttemptHint = hint
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save task: %v", prd.ID, t.ID, err)
	}
	m.appendProtectedPathsDecision(r, protectedPathsRejectedDecisionKind,
		fmt.Sprintf("task %s: restored %d protected path(s) to %s%s: %s; the task runs again",
			t.ID, len(paths), shortSHA(base), restoreCommitNote(sha), list))
	m.auditProtectedPathsAnswer(r)
	log.Printf("[autonomous] %s/%s protected-paths: operator rejected, restored %d path(s)%s", prd.ID, t.ID, len(paths), restoreCommitNote(sha))
	return true
}

// restoreCommitNote names the restore commit in the decision note, or says
// nothing was left to commit.
func restoreCommitNote(sha string) string {
	if sha == "" {
		return " (nothing to commit)"
	}
	return " in commit " + shortSHA(sha)
}

// pathInTree reports whether path exists in the tree of rev in dir. A git
// failure is an error, never "absent", so a transient failure cannot turn a
// restore into a removal.
func pathInTree(ctx context.Context, run driftGitRunner, dir, rev, path string) (bool, error) {
	out, err := run(ctx, dir, "ls-tree", "--name-only", rev, "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// failProtectedPathsReject keeps the hold after a restore that did not happen.
// It records the reason on the hold and in the decision log, raises the warn
// alert and, so the operator can answer again, raises one fresh question that
// the hold then names. It always reports false: the task is not released.
func (m *Manager) failProtectedPathsReject(r *protectedPathsAnswer, cause string) bool {
	prd, t := r.prd, r.t
	m.appendProtectedPathsDecision(r, protectedPathsRejectedDecisionKind,
		"task "+t.ID+": "+cause+"; the hold stays")

	m.mu.Lock()
	alertFn := m.alertSink
	m.mu.Unlock()
	if alertFn != nil {
		alertFn("warn", "Protected paths not restored",
			"Automaton "+prd.ID+" ("+prd.Title+"), task "+t.ID+" ("+t.Title+"): "+cause+
				". The hold stays. Paths: "+boundedFileList(r.hold.Paths, protectedPathsQuestionFiles))
	}

	questionID := m.raiseProtectedPathsQuestion(prd, t, r.hold.Paths, r.hold.BaseCommit)
	m.store.WithLock(func() {
		h := t.ProtectedPathHold
		if h == nil {
			return
		}
		h.QuestionID = questionID
		h.Reason = cause
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save task: %v", prd.ID, t.ID, err)
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s protected-paths: save Automaton: %v", prd.ID, err)
	}
	m.auditProtectedPathsAnswer(r)
	m.emitTaskStatus(prd, t, TaskBlocked, nil, nil)
	return false
}

// answerProtectedPathsChangeSpec keeps the task blocked and marks the hold as
// awaiting a change to the task's instructions. The reset the operator does
// after editing clears the hold, and the gate runs again on the next attempt.
func (m *Manager) answerProtectedPathsChangeSpec(r *protectedPathsAnswer) {
	prd, t := r.prd, r.t
	m.store.WithLock(func() {
		if t.ProtectedPathHold != nil {
			t.ProtectedPathHold.AwaitingSpecChange = true
		}
		t.Error = protectedPathsSpecChangeError
		t.UpdatedAt = time.Now()
	})
	m.appendProtectedPathsDecision(r, protectedPathsSpecChangeDecisionKind,
		"task "+t.ID+": the operator will change the task instructions; protected paths: "+
			boundedFileList(r.hold.Paths, protectedPathsQuestionFiles))
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save task: %v", prd.ID, t.ID, err)
	}
	m.auditProtectedPathsAnswer(r)
	m.emitTaskStatus(prd, t, TaskBlocked, nil, nil)
	log.Printf("[autonomous] %s/%s protected-paths: waiting for a spec change", prd.ID, t.ID)
}

// appendProtectedPathsDecision adds one Decision to the Automaton's log under
// the answering actor, and saves the Automaton.
func (m *Manager) appendProtectedPathsDecision(r *protectedPathsAnswer, kind, note string) {
	m.store.WithLock(func() {
		r.prd.Decisions = append(r.prd.Decisions, Decision{
			At: time.Now(), Kind: kind, Actor: r.actor,
			Note: note, QuestionID: r.question.ID,
		})
	})
	if err := m.store.SavePRD(r.prd); err != nil {
		log.Printf("[autonomous] %s protected-paths: save Automaton: %v", r.prd.ID, err)
	}
}

// auditProtectedPathsAnswer audits autonomous_protected_paths_answered with
// the chosen option and the held paths.
func (m *Manager) auditProtectedPathsAnswer(r *protectedPathsAnswer) {
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn == nil {
		return
	}
	auditFn(protectedPathsAnsweredAuditAction, map[string]any{
		"prd_id":      r.prd.ID,
		"task_id":     r.t.ID,
		"option":      r.option,
		"actor":       r.actor,
		"question_id": r.question.ID,
		"paths":       r.hold.Paths,
	})
}
