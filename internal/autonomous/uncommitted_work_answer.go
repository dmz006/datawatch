// Operator answer to the uncommitted-work question.
//
// The hold code (uncommitted_work_hold.go) parks a task, or an Automaton, on a
// question with the options commit, discard and retry. This file acts on the
// answer. Only the question the hold itself names is acted on: a question
// whose id does not match the hold (the hold was replaced, cleared or already
// resolved) is stale and changes nothing, and so does a withdrawn or expired
// question, which keeps the hold.
//
//	commit   stages and commits exactly the listed files that are still
//	         uncommitted, including the ones outside the task's declared files
//	         (the operator chose it), then completes the task, or lets the
//	         Automaton complete.
//	discard  removes exactly the listed files that are still uncommitted, and
//	         nothing else, then runs the task again or lets the Automaton roll
//	         up again.
//	retry    touches no file: the task runs again, or the roll-up looks at the
//	         worktree again (and holds again when files remain).
//
// A failed commit or discard leaves the hold in place and raises a fresh
// question, so the operator is never left without a way to answer.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Kinds of Decision and audit event the answer writes.
const (
	uncommittedResolvedDecisionKind       = "uncommitted_work_resolved"
	uncommittedDiscardedDecisionKind      = "uncommitted_work_discarded"
	uncommittedCommitFailedDecisionKind   = "uncommitted_work_commit_failed"
	uncommittedDiscardRefusedDecisionKind = "uncommitted_work_discard_refused"
	uncommittedDiscardFailedDecisionKind  = "uncommitted_work_discard_failed"
	uncommittedResolveFailedDecisionKind  = "uncommitted_work_resolve_failed"
	uncommittedResolvedAuditAction        = "autonomous_uncommitted_work_resolved"
)

// uncommittedAnswerTimeout bounds the git work one answer does.
const uncommittedAnswerTimeout = 5 * time.Minute

// discardFilesFn removes the listed changes from dir: tracked files are put
// back to HEAD (index and work tree), untracked files are deleted. Paths are
// relative to dir and already checked to lie inside it. The Manager takes it as
// a field so tests can substitute a fake.
type discardFilesFn func(ctx context.Context, dir string, tracked, untracked []string) error

// defaultDiscardFiles is the discardFilesFn that shells out to git. It touches
// only the listed paths: an explicit pathspec on git restore, and a single file
// delete for each untracked path.
func defaultDiscardFiles(ctx context.Context, dir string, tracked, untracked []string) error {
	if dir == "" {
		return errors.New("discard: empty directory")
	}
	for start := 0; start < len(tracked); start += snapshotAddChunk {
		end := min(start+snapshotAddChunk, len(tracked))
		args := append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, tracked[start:end]...)
		if out, err := runGitInCtx(ctx, dir, args...); err != nil {
			return fmt.Errorf("discard: git restore in %s: %w: %s", dir, err, strings.TrimSpace(out))
		}
	}
	for _, rel := range untracked {
		full, err := resolveInsideDir(dir, rel)
		if err != nil {
			return err
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("discard: remove %s: %w", rel, err)
		}
	}
	return nil
}

// discardFilesFunc returns the discard function in effect.
func (m *Manager) discardFilesFunc() discardFilesFn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.discardFiles == nil {
		return defaultDiscardFiles
	}
	return m.discardFiles
}

// resolveInsideDir returns the absolute path of rel under dir, or an error
// when rel is empty, absolute, names the git directory, or leaves dir by a ".."
// segment or through a symbolic link in one of its parent directories.
func resolveInsideDir(dir, rel string) (string, error) {
	if dir == "" || rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is not a path inside the task directory", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	sep := string(filepath.Separator)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+sep) {
		return "", fmt.Errorf("path %q is outside the task directory", rel)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git"+sep) {
		return "", fmt.Errorf("path %q is inside the git directory", rel)
	}
	root := filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	full := filepath.Join(root, clean)
	if parent, err := filepath.EvalSymlinks(filepath.Dir(full)); err == nil {
		if parent != root && !strings.HasPrefix(parent, root+sep) {
			return "", fmt.Errorf("path %q is outside the task directory", rel)
		}
		full = filepath.Join(parent, filepath.Base(full))
	}
	return full, nil
}

// uncommittedAnswer is one answered hold: the Automaton, the held task (nil for
// a worktree hold), a copy of the hold and who answered.
type uncommittedAnswer struct {
	prd      *PRD
	t        *Task
	hold     UncommittedWorkHold
	question Question
	actor    string
	dir      string
}

// onUncommittedWorkAnswered acts on the operator's answer to an
// uncommitted-work question (see the file comment) and then lets the
// Automaton run on, the way ResolveTaskReview does: a live executor is woken
// to re-scan and no second one is started; otherwise the run is relaunched.
func (a *API) onUncommittedWorkAnswered(c QuestionChange) {
	prdID, changed := a.M.resolveUncommittedWork(c)
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
			log.Printf("[autonomous] %s run after uncommitted-work answer: %v", prdID, err)
		}
	}
}

// resolveUncommittedWork acts on an answered uncommitted-work question. It
// returns the Automaton id the question belongs to ("" when the change is not
// one to act on) and whether the hold was resolved, which is when the Automaton
// should run on.
func (m *Manager) resolveUncommittedWork(c QuestionChange) (prdID string, resolved bool) {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || q.Label != UncommittedQuestionLabel || c.Answer == nil {
		return "", false
	}
	if len(q.ID) == 0 || len(q.PRDID) == 0 {
		return "", false
	}
	option := c.Answer.ChosenOptionID
	switch option {
	case UncommittedOptionCommit, UncommittedOptionDiscard, UncommittedOptionRetry:
	default:
		// The default "hold" (or anything else) keeps the hold as it is.
		return "", false
	}
	prd, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return "", false
	}
	r := &uncommittedAnswer{prd: prd, question: q, actor: c.Answer.Actor}
	if r.actor == "" {
		r.actor = "operator"
	}
	wantKind := UncommittedHoldKindWorktree
	if q.TaskID != "" {
		wantKind = UncommittedHoldKindTask
		if r.t, _ = findTaskInPRD(prd, q.TaskID); r.t == nil {
			return "", false
		}
	}
	named := false
	m.store.WithLock(func() {
		h := prd.UncommittedWork
		if r.t != nil {
			h = r.t.UncommittedWork
		}
		if h == nil || h.QuestionID != q.ID || h.Kind != wantKind {
			return
		}
		if r.t != nil && r.t.Status != TaskBlocked {
			return
		}
		r.hold = *h
		r.hold.Files = append([]string(nil), h.Files...)
		r.hold.OutsideScope = append([]string(nil), h.OutsideScope...)
		named = true
	})
	// A question the hold does not name is stale.
	if !named {
		return "", false
	}
	if r.t != nil {
		r.dir = m.resolveTaskProjectDir(prd, r.t)
	} else {
		r.dir = prd.ProjectDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), uncommittedAnswerTimeout)
	defer cancel()
	switch option {
	case UncommittedOptionCommit:
		resolved = m.answerUncommittedCommit(ctx, r)
	case UncommittedOptionDiscard:
		resolved = m.answerUncommittedDiscard(ctx, r)
	default:
		resolved = m.answerUncommittedRetry(r)
	}
	return prd.ID, resolved
}

// stillUncommitted narrows detected to the files the hold names, sorted by path.
func stillUncommitted(hold []string, detected []UncommittedFile) []UncommittedFile {
	named := make(map[string]bool, len(hold))
	for _, p := range hold {
		named[p] = true
	}
	var out []UncommittedFile
	for _, f := range detected {
		if named[f.Path] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// holdFiles rebuilds the file list of a hold for a new question.
func holdFiles(h UncommittedWorkHold) []UncommittedFile {
	outside := make(map[string]bool, len(h.OutsideScope))
	for _, p := range h.OutsideScope {
		outside[p] = true
	}
	files := make([]UncommittedFile, 0, len(h.Files))
	for _, p := range h.Files {
		files = append(files, UncommittedFile{Path: p, OutsideScope: outside[p]})
	}
	return files
}

// scopeName is the one-line name of what is held, for notes and alerts.
func (r *uncommittedAnswer) scopeName() string {
	if r.t != nil {
		return "task " + r.t.ID
	}
	return "worktree"
}

// answerUncommittedCommit is the commit answer. It reports whether the hold was
// resolved.
func (m *Manager) answerUncommittedCommit(ctx context.Context, r *uncommittedAnswer) bool {
	if r.dir == "" {
		return m.failUncommittedAnswer(r, uncommittedCommitFailedDecisionKind, nil,
			UncommittedCauseCommitFailed+": the "+r.scopeName()+" has no directory")
	}
	detected, err := DetectUncommittedWork(ctx, r.dir, m.driftGit)
	if err != nil {
		return m.failUncommittedAnswer(r, uncommittedCommitFailedDecisionKind, nil, uncommittedCommitCause(err))
	}
	still := stillUncommitted(r.hold.Files, detected)
	var sha string
	if len(still) > 0 {
		paths := make([]string, len(still))
		for i, f := range still {
			paths[i] = f.Path
		}
		sha, err = m.taskCommitFn()(ctx, r.dir, paths, uncommittedAnswerCommitMessage(r))
		if err != nil {
			log.Printf("[autonomous] WARNING %s %s uncommitted-work: commit failed: %v", r.prd.ID, r.scopeName(), err)
			return m.failUncommittedAnswer(r, uncommittedCommitFailedDecisionKind, still, uncommittedCommitCause(err))
		}
		log.Printf("[autonomous] %s %s uncommitted-work: operator committed %d file(s) as %s", r.prd.ID, r.scopeName(), len(paths), shortID(sha))
	}
	note := fmt.Sprintf("committed %d file(s)", len(still))
	if r.t == nil {
		m.clearAutomatonUncommittedHold(r, UncommittedOptionCommit, note)
		return true
	}
	return m.completeHeldTask(ctx, r, note)
}

// uncommittedAnswerCommitMessage is the subject of the commit the operator
// asked for: the task's own style for a task hold.
func uncommittedAnswerCommitMessage(r *uncommittedAnswer) string {
	if r.t != nil {
		return uncommittedTaskCommitMessage(r.prd, r.t)
	}
	return "task(" + shortID(r.prd.ID) + "): commit leftover worktree files"
}

// completeHeldTask finishes a task whose files the operator just committed.
// It looks at the tree again first: files that are still uncommitted hold the
// task again under the configured policy, the way the completion check does.
func (m *Manager) completeHeldTask(ctx context.Context, r *uncommittedAnswer, note string) bool {
	prd, t := r.prd, r.t
	auto := m.Config().Automation
	policy := auto.EffectiveUncommittedWorkPolicy()
	if policy != config.UncommittedWorkPolicyOff {
		detected, err := DetectUncommittedWork(ctx, r.dir, m.driftGit)
		if err == nil {
			scoped := ScopeUncommittedFiles(detected, t, EffectiveTaskWriteDirs(prd, t), r.dir, auto.EffectiveUncommittedWorkScope())
			if len(scoped) > 0 {
				m.recordUncommittedResolved(r, UncommittedOptionCommit, note+"; files are still uncommitted")
				if policy == config.UncommittedWorkPolicyWarn {
					m.warnUncommittedWork(prd, t, scoped, UncommittedCauseStillDirty)
					return m.finishHeldTask(ctx, r)
				}
				if herr := m.holdForUncommittedWork(prd, t, scoped, UncommittedCauseStillDirty); herr != nil {
					log.Printf("[autonomous] %s/%s uncommitted-work: hold again: %v", prd.ID, t.ID, herr)
				}
				return false
			}
		}
	}
	m.recordUncommittedResolved(r, UncommittedOptionCommit, note)
	return m.finishHeldTask(ctx, r)
}

// finishHeldTask marks the held task completed the way the executor does
// (CompletedAt, post-verify SHA from HEAD, TaskCompleted), clears the hold and
// reopens a blocked Automaton so the next run rolls it up.
func (m *Manager) finishHeldTask(ctx context.Context, r *uncommittedAnswer) bool {
	prd, t := r.prd, r.t
	postSHA := CapturePostVerifySHA(ctx, r.dir, m.driftGit)
	done := time.Now()
	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.UncommittedWork = nil
		t.Error = ""
		if t.FailureReason != nil && t.FailureReason.Source == "uncommitted-work-hold" {
			t.FailureReason = nil
		}
		t.CompletedAt = &done
		t.PostVerifySHA = postSHA
		t.Status = TaskCompleted
		t.UpdatedAt = done
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s uncommitted-work: save task: %v", prd.ID, t.ID, err)
	}
	m.reopenAfterUncommittedAnswer(prd)
	m.emitTaskStatus(prd, t, old, nil, nil)
	return true
}

// clearAutomatonUncommittedHold clears the worktree hold and reopens a blocked
// Automaton so the next run rolls it up again, which looks at the worktree
// once more and holds again when files remain.
func (m *Manager) clearAutomatonUncommittedHold(r *uncommittedAnswer, option, note string) {
	m.store.WithLock(func() { r.prd.UncommittedWork = nil })
	m.recordUncommittedResolved(r, option, note)
	m.reopenAfterUncommittedAnswer(r.prd)
}

// reopenAfterUncommittedAnswer returns a blocked Automaton to running and saves
// it, announcing the status change. A running Automaton is saved as it is; any
// other status is left alone.
func (m *Manager) reopenAfterUncommittedAnswer(prd *PRD) {
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
		log.Printf("[autonomous] %s uncommitted-work: save Automaton: %v", prd.ID, err)
	}
	if changed {
		m.emitPRDStatus(prd, old, lastDecision(prd, uncommittedResolvedDecisionKind), nil)
	}
}

// answerUncommittedDiscard is the discard answer. It reports whether the hold
// was resolved.
func (m *Manager) answerUncommittedDiscard(ctx context.Context, r *uncommittedAnswer) bool {
	if r.dir == "" {
		return m.failUncommittedAnswer(r, uncommittedDiscardRefusedDecisionKind, nil,
			"the files were not discarded: the "+r.scopeName()+" has no directory")
	}
	detected, err := DetectUncommittedWork(ctx, r.dir, m.driftGit)
	if err != nil {
		return m.failUncommittedAnswer(r, uncommittedDiscardFailedDecisionKind, nil,
			"the files were not discarded: "+uncommittedCommitCause(err))
	}
	still := stillUncommitted(r.hold.Files, detected)

	var tracked, untracked []string
	for _, f := range still {
		if _, perr := resolveInsideDir(r.dir, f.Path); perr != nil {
			// Nothing is deleted when any listed path is not inside the task
			// directory.
			return m.failUncommittedAnswer(r, uncommittedDiscardRefusedDecisionKind, still,
				"the files were not discarded: "+perr.Error())
		}
		if f.Status == "??" {
			untracked = append(untracked, f.Path)
		} else {
			tracked = append(tracked, f.Path)
		}
	}
	if len(still) > 0 {
		if derr := m.discardFilesFunc()(ctx, r.dir, tracked, untracked); derr != nil {
			log.Printf("[autonomous] WARNING %s %s uncommitted-work: discard failed: %v", r.prd.ID, r.scopeName(), derr)
			return m.failUncommittedAnswer(r, uncommittedDiscardFailedDecisionKind, still,
				"the files were not discarded: "+uncommittedCommitCause(derr))
		}
	}
	paths := make([]string, len(still))
	for i, f := range still {
		paths[i] = f.Path
	}
	list := "none left to discard"
	if len(paths) > 0 {
		list = boundedFileList(paths, uncommittedQuestionFiles)
	}
	m.appendUncommittedDecisionAs(r, uncommittedDiscardedDecisionKind, r.scopeName()+": discarded "+list)
	log.Printf("[autonomous] %s %s uncommitted-work: operator discarded %d file(s)", r.prd.ID, r.scopeName(), len(paths))

	note := fmt.Sprintf("discarded %d file(s)", len(paths))
	if r.t == nil {
		m.clearAutomatonUncommittedHold(r, UncommittedOptionDiscard, note)
		return true
	}
	return m.resetHeldTask(r, UncommittedOptionDiscard, note)
}

// answerUncommittedRetry is the retry answer: no file is touched. It reports
// whether the hold was resolved.
func (m *Manager) answerUncommittedRetry(r *uncommittedAnswer) bool {
	if r.t == nil {
		m.clearAutomatonUncommittedHold(r, UncommittedOptionRetry, "the worktree is checked again")
		return true
	}
	return m.resetHeldTask(r, UncommittedOptionRetry, "the task runs again")
}

// resetHeldTask sends the held task back to pending through resetTask, the path
// reset_task and the review retry use, so the attempt history is kept. resetTask
// drops the hold.
func (m *Manager) resetHeldTask(r *uncommittedAnswer, option, note string) bool {
	if _, err := m.resetTask(r.prd.ID, r.t.ID, r.actor, false, false, nil); err != nil {
		log.Printf("[autonomous] WARNING %s/%s uncommitted-work: reset task: %v", r.prd.ID, r.t.ID, err)
		return m.failUncommittedAnswer(r, uncommittedResolveFailedDecisionKind, nil,
			"the task could not be run again: "+uncommittedCommitCause(err))
	}
	m.recordUncommittedResolved(r, option, note)
	return true
}

// appendUncommittedDecisionAs adds one Decision to the Automaton's log under
// the answering actor, and saves it.
func (m *Manager) appendUncommittedDecisionAs(r *uncommittedAnswer, kind, note string) {
	m.store.WithLock(func() {
		r.prd.Decisions = append(r.prd.Decisions, Decision{
			At: time.Now(), Kind: kind, Actor: r.actor,
			Note: note, QuestionID: r.question.ID,
		})
	})
	if err := m.store.SavePRD(r.prd); err != nil {
		log.Printf("[autonomous] %s uncommitted-work: save Automaton: %v", r.prd.ID, err)
	}
}

// recordUncommittedResolved appends the uncommitted_work_resolved Decision with
// the chosen option and audits autonomous_uncommitted_work_resolved.
func (m *Manager) recordUncommittedResolved(r *uncommittedAnswer, option, note string) {
	m.appendUncommittedDecisionAs(r, uncommittedResolvedDecisionKind,
		fmt.Sprintf("%s: option=%s; %s", r.scopeName(), option, note))
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn == nil {
		return
	}
	details := map[string]any{
		"prd_id":      r.prd.ID,
		"option":      option,
		"actor":       r.actor,
		"question_id": r.question.ID,
		"files":       r.hold.Files,
	}
	if r.t != nil {
		details["task_id"] = r.t.ID
	}
	auditFn(uncommittedResolvedAuditAction, details)
}

// failUncommittedAnswer keeps the hold after a commit or discard that did not
// happen. It appends a Decision of the given kind, raises the warn alert and,
// so the operator can answer again, raises a fresh question that the hold then
// names. files, when set, are the files the new question lists; otherwise the
// hold's own list is used. It always reports false: the hold is not resolved.
func (m *Manager) failUncommittedAnswer(r *uncommittedAnswer, kind string, files []UncommittedFile, cause string) bool {
	if len(files) == 0 {
		files = holdFiles(r.hold)
	}
	m.appendUncommittedDecisionAs(r, kind, r.scopeName()+": "+cause)

	m.mu.Lock()
	alertFn := m.alertSink
	m.mu.Unlock()
	if alertFn != nil {
		body := "Automaton " + r.prd.ID + " (" + r.prd.Title + ")"
		if r.t != nil {
			body += ", task " + r.t.ID + " (" + r.t.Title + ")"
		}
		all, _ := uncommittedFilePaths(files)
		body += ": " + cause + ". The hold stays. Files: " + boundedFileList(all, uncommittedQuestionFiles)
		alertFn("warn", "Uncommitted work not resolved", body)
	}

	questionID := m.raiseUncommittedQuestion(r.prd, r.t, r.hold.Kind, files, cause)
	all, outside := uncommittedFilePaths(files)
	m.store.WithLock(func() {
		h := r.prd.UncommittedWork
		if r.t != nil {
			h = r.t.UncommittedWork
		}
		if h == nil {
			return
		}
		h.QuestionID = questionID
		h.Reason = cause
		h.Files = all
		h.OutsideScope = outside
	})
	if r.t != nil {
		if err := m.store.SaveTask(r.t); err != nil {
			log.Printf("[autonomous] %s/%s uncommitted-work: save task: %v", r.prd.ID, r.t.ID, err)
		}
	}
	if err := m.store.SavePRD(r.prd); err != nil {
		log.Printf("[autonomous] %s uncommitted-work: save Automaton: %v", r.prd.ID, err)
	}
	return false
}
