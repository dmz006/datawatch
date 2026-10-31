// Uncommitted-work hold: the record a held task or Automaton carries, and the
// code that holds, warns and asks.
//
// After a task finishes in an Automaton that commits its own work
// (prdAutoCommits), the worktree must be clean. When the task commit failed or
// files are still uncommitted after it, the policy decides what happens.
// holdForUncommittedWork parks the task in TaskBlocked and raises one operator
// question; holdAutomatonForUncommittedWork records the same for files no
// single task owns and leaves the statuses to the roll-up; warnUncommittedWork
// only records and alerts. The question's default is "hold": nothing changes
// until the operator answers commit, discard or retry.
package autonomous

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Kinds of Decision and audit event the uncommitted-work hold writes.
const (
	uncommittedHeldDecisionKind   = "uncommitted_work_held"
	uncommittedWarnedDecisionKind = "uncommitted_work_warned"
	uncommittedHeldAuditAction    = "autonomous_uncommitted_work_held"
	uncommittedWarnedAuditAction  = "autonomous_uncommitted_work_warned"
)

// UncommittedWorkHold.Kind values.
const (
	UncommittedHoldKindTask     = "task"
	UncommittedHoldKindWorktree = "worktree"
)

// Question label and option ids of the uncommitted-work question.
const (
	UncommittedQuestionLabel   = "uncommitted_work"
	UncommittedOptionCommit    = "commit"
	UncommittedOptionDiscard   = "discard"
	UncommittedOptionRetry     = "retry"
	uncommittedQuestionDefault = "hold"
)

// Causes the caller passes for a hold or a warning.
const (
	UncommittedCauseCommitFailed = "the commit failed"
	UncommittedCauseStillDirty   = "files were still uncommitted after the commit"
)

const (
	// uncommittedQuestionFiles caps the files listed in the question text.
	uncommittedQuestionFiles = 20
	// uncommittedErrorFiles caps the files named in Task.Error.
	uncommittedErrorFiles = 5
)

// uncommittedFilePaths splits files into the sorted paths of all of them and
// of the ones outside the task's declared scope.
func uncommittedFilePaths(files []UncommittedFile) (all, outside []string) {
	for _, f := range files {
		all = append(all, f.Path)
		if f.OutsideScope {
			outside = append(outside, f.Path)
		}
	}
	sort.Strings(all)
	sort.Strings(outside)
	return all, outside
}

// boundedFileList joins at most limit paths with ", " and appends "and N more"
// for the rest.
func boundedFileList(paths []string, limit int) string {
	if len(paths) <= limit {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:limit], ", "), len(paths)-limit)
}

// uncommittedQuestionText is the question the operator reads. It names the
// Automaton, the task (when there is one), the cause and the files, and says
// which files are outside the task's declared files.
func uncommittedQuestionText(prd *PRD, t *Task, files []UncommittedFile, cause string) string {
	var b strings.Builder
	if t != nil {
		fmt.Fprintf(&b, "Task %q (%s) of Automaton %q (%s) finished, but its worktree still has uncommitted files.",
			t.Title, t.ID, prd.Title, prd.ID)
	} else {
		fmt.Fprintf(&b, "The worktree of Automaton %q (%s) still has uncommitted files.", prd.Title, prd.ID)
	}
	fmt.Fprintf(&b, "\nCause: %s.\n\nFiles:", cause)
	sorted := append([]UncommittedFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i, f := range sorted {
		if i == uncommittedQuestionFiles {
			fmt.Fprintf(&b, "\n- and %d more", len(sorted)-uncommittedQuestionFiles)
			break
		}
		fmt.Fprintf(&b, "\n- %s", f.Path)
		if f.OutsideScope {
			b.WriteString(" (outside the task's declared files)")
		}
	}
	b.WriteString("\n\nNothing changes until you answer.")
	text := b.String()
	if len(text) > questionTextMaxBytes {
		text = text[:questionTextMaxBytes]
	}
	return text
}

// uncommittedQuestion builds the question for a hold. kind is the hold kind:
// a task hold carries the task context, a worktree hold the Automaton context.
func uncommittedQuestion(prd *PRD, t *Task, kind string, files []UncommittedFile, cause string) Question {
	retryLabel := "Check again"
	q := Question{
		Label:     UncommittedQuestionLabel,
		AskedBy:   "autonomous",
		AskerKind: QuestionAskerSystem,
		PRDID:     prd.ID,
		Context:   QuestionContextPrd,
		Text:      uncommittedQuestionText(prd, t, files, cause),
		Default:   uncommittedQuestionDefault,
	}
	if kind == UncommittedHoldKindTask && t != nil {
		retryLabel = "Retry the task"
		q.TaskID = t.ID
		q.Context = QuestionContextTask
	}
	q.Options = []QuestionOption{
		{ID: UncommittedOptionCommit, Label: "Commit them", Tradeoffs: "Stages exactly the listed files and commits them to the Automaton branch."},
		{ID: UncommittedOptionDiscard, Label: "Discard them", Tradeoffs: "Removes the listed changes from the worktree. They cannot be recovered."},
		{ID: UncommittedOptionRetry, Label: retryLabel, Tradeoffs: "Looks at the worktree again and runs the check once more."},
	}
	_, outside := uncommittedFilePaths(files)
	if len(outside) == 0 {
		q.Recommendation = &QuestionRecommendation{
			OptionID:  UncommittedOptionCommit,
			Rationale: "Every file is inside the task's declared files.",
		}
	} else {
		q.Recommendation = &QuestionRecommendation{
			OptionID:  UncommittedOptionRetry,
			Rationale: fmt.Sprintf("%d file(s) are outside the task's declared files; check them before committing.", len(outside)),
		}
	}
	return q
}

// openUncommittedQuestion reports whether id names a question that is still
// open in the Manager's question store.
func (m *Manager) openUncommittedQuestion(id string) bool {
	if id == "" {
		return false
	}
	store, _, _ := m.decisionGateSettings()
	if store == nil {
		return false
	}
	q, ok := store.Get(id)
	return ok && q.State == QuestionOpen
}

// raiseUncommittedQuestion raises the question through the store the decision
// gate holds and returns its id. With no store, or when raising fails, it
// returns "" and the hold stands without a question.
func (m *Manager) raiseUncommittedQuestion(prd *PRD, t *Task, kind string, files []UncommittedFile, cause string) string {
	store, _, _ := m.decisionGateSettings()
	if store == nil {
		return ""
	}
	q, err := store.Raise(uncommittedQuestion(prd, t, kind, files, cause))
	if err != nil {
		log.Printf("[autonomous] prd=%s uncommitted-work: raise question: %v", prd.ID, err)
		return ""
	}
	return q.ID
}

// recordUncommittedEvent audits and alerts one uncommitted-work event, both
// nil-safe as checkUndeclaredEdits does.
func (m *Manager) recordUncommittedEvent(prd *PRD, t *Task, files []UncommittedFile, cause, action, title string) {
	all, outside := uncommittedFilePaths(files)
	m.mu.Lock()
	auditFn := m.auditFn
	alertFn := m.alertSink
	m.mu.Unlock()
	if auditFn != nil {
		details := map[string]any{
			"prd_id":        prd.ID,
			"files":         all,
			"outside_scope": outside,
			"cause":         cause,
		}
		if t != nil {
			details["task_id"] = t.ID
		}
		auditFn(action, details)
	}
	if alertFn != nil {
		body := "Automaton " + prd.ID + " (" + prd.Title + ")"
		if t != nil {
			body += ", task " + t.ID + " (" + t.Title + ")"
		}
		body += ": " + cause + ". Files: " + boundedFileList(all, uncommittedQuestionFiles)
		alertFn("warn", title, body)
	}
}

// appendUncommittedDecision adds one Decision to the Automaton's log.
func (m *Manager) appendUncommittedDecision(prd *PRD, kind, note, questionID string) {
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: kind, Actor: "autonomous",
			Note: note, QuestionID: questionID,
		})
	})
}

// uncommittedNote is the one-line Decision note.
func uncommittedNote(prd *PRD, t *Task, files []UncommittedFile, cause string) string {
	all, outside := uncommittedFilePaths(files)
	note := ""
	if t != nil {
		note = "task " + t.ID + ": "
	}
	note += cause + "; uncommitted: " + boundedFileList(all, uncommittedQuestionFiles)
	if len(outside) > 0 {
		note += "; outside the declared files: " + boundedFileList(outside, uncommittedQuestionFiles)
	}
	return note
}

// holdForUncommittedWork parks t in TaskBlocked because files are still
// uncommitted in its worktree, the way the setup-fault hold parks a task. It
// records the hold on the task and the Automaton decision log, audits and
// alerts, raises one question (default hold) and saves and announces the
// status. A task that already carries a hold whose question is still open is
// left alone: no second question is raised. With no question store the task is
// still held and QuestionID stays empty.
func (m *Manager) holdForUncommittedWork(prd *PRD, t *Task, files []UncommittedFile, cause string) error {
	var existing string
	m.store.WithLock(func() {
		if t.UncommittedWork != nil {
			existing = t.UncommittedWork.QuestionID
		}
	})
	if m.openUncommittedQuestion(existing) {
		return nil
	}

	all, outside := uncommittedFilePaths(files)
	questionID := m.raiseUncommittedQuestion(prd, t, UncommittedHoldKindTask, files, cause)
	detail := uncommittedNote(prd, t, files, cause)

	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.Status = TaskBlocked
		t.Error = "uncommitted_work: " + boundedFileList(all, uncommittedErrorFiles)
		t.UncommittedWork = &UncommittedWorkHold{
			Kind:         UncommittedHoldKindTask,
			Policy:       config.UncommittedWorkPolicyHold,
			Files:        all,
			OutsideScope: outside,
			Reason:       cause,
			QuestionID:   questionID,
			HeldAt:       time.Now(),
		}
	})
	t.FailureReason = &FailureReason{
		Category: "git", Source: "uncommitted-work-hold", Retryable: true,
		Detail: detail, OccurredAt: time.Now(),
	}
	m.appendUncommittedDecision(prd, uncommittedHeldDecisionKind, detail, questionID)
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s uncommitted-work: save Automaton: %v", prd.ID, t.ID, err)
	}
	m.recordUncommittedEvent(prd, t, files, cause, uncommittedHeldAuditAction, "Uncommitted work held")
	log.Printf("[autonomous] WARNING %s/%s held: uncommitted work (%s): %s", prd.ID, t.ID, cause, boundedFileList(all, uncommittedErrorFiles))
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	return nil
}

// holdAutomatonForUncommittedWork is the worktree sibling of
// holdForUncommittedWork: files that no single task owns. It records the hold
// on the Automaton, the decision, the audit event and the alert and raises the
// question. It changes no status; the roll-up task does. An Automaton that
// already carries a hold whose question is still open is left alone.
func (m *Manager) holdAutomatonForUncommittedWork(prd *PRD, files []UncommittedFile, cause string) {
	var existing string
	m.store.WithLock(func() {
		if prd.UncommittedWork != nil {
			existing = prd.UncommittedWork.QuestionID
		}
	})
	if m.openUncommittedQuestion(existing) {
		return
	}

	all, outside := uncommittedFilePaths(files)
	questionID := m.raiseUncommittedQuestion(prd, nil, UncommittedHoldKindWorktree, files, cause)
	m.store.WithLock(func() {
		prd.UncommittedWork = &UncommittedWorkHold{
			Kind:         UncommittedHoldKindWorktree,
			Policy:       config.UncommittedWorkPolicyHold,
			Files:        all,
			OutsideScope: outside,
			Reason:       cause,
			QuestionID:   questionID,
			HeldAt:       time.Now(),
		}
	})
	m.appendUncommittedDecision(prd, uncommittedHeldDecisionKind, uncommittedNote(prd, nil, files, cause), questionID)
	// An executor-owned save: the roll-up commits the blocked status right
	// after this and must not read its own revision bump as a concurrent write.
	if err := m.savePRDOwn(prd); err != nil {
		log.Printf("[autonomous] %s uncommitted-work: save Automaton: %v", prd.ID, err)
	}
	m.recordUncommittedEvent(prd, nil, files, cause, uncommittedHeldAuditAction, "Uncommitted work held")
	log.Printf("[autonomous] WARNING %s held: uncommitted work in the worktree (%s): %s", prd.ID, cause, boundedFileList(all, uncommittedErrorFiles))
}

// warnUncommittedWork is the warn policy: it records the uncommitted_work_warned
// decision, audits and raises a warn alert. It changes no status and asks no
// question. t may be nil for the Automaton-wide warning.
func (m *Manager) warnUncommittedWork(prd *PRD, t *Task, files []UncommittedFile, cause string) {
	m.appendUncommittedDecision(prd, uncommittedWarnedDecisionKind, uncommittedNote(prd, t, files, cause), "")
	save := m.store.SavePRD
	if t == nil {
		// The Automaton-wide warning comes from the roll-up, which completes
		// the Automaton right after it: count the save as the executor's own.
		save = m.savePRDOwn
	}
	if err := save(prd); err != nil {
		log.Printf("[autonomous] %s uncommitted-work: save Automaton: %v", prd.ID, err)
	}
	m.recordUncommittedEvent(prd, t, files, cause, uncommittedWarnedAuditAction, "Uncommitted work warning")
	all, _ := uncommittedFilePaths(files)
	log.Printf("[autonomous] WARNING %s uncommitted work (%s): %s", prd.ID, cause, boundedFileList(all, uncommittedErrorFiles))
}
