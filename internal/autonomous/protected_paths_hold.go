// Protected-paths hold: the record a held task carries, the operator question
// and the code that holds, warns and blocks.
//
// After a task's commit, the protected-paths gate compares the changed paths
// with the protected-path patterns. This file is what happens to a task whose
// commit changed protected paths the Automaton has not authorised.
// holdForProtectedPaths parks the task in TaskBlocked and raises one operator
// question whose default is "hold": nothing changes until the operator answers
// approve, reject or change the spec. warnProtectedPaths only records and
// alerts. blockProtectedPaths blocks the task without a question: the operator
// authorises the path with the Automaton's authorised paths, or edits the task,
// then resets it. The gate check itself lives elsewhere; this file only records
// the outcome.
package autonomous

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/config"
)

// Kinds of Decision and audit event the protected-paths hold writes.
const (
	protectedPathsHeldDecisionKind       = "protected_paths_held"
	protectedPathsWarnedDecisionKind     = "protected_paths_warned"
	protectedPathsBlockedDecisionKind    = "protected_paths_blocked"
	protectedPathsAuthorisedDecisionKind = "protected_paths_authorised"

	protectedPathsHeldAuditAction    = "autonomous_protected_paths_held"
	protectedPathsWarnedAuditAction  = "autonomous_protected_paths_warned"
	protectedPathsBlockedAuditAction = "autonomous_protected_paths_blocked"
)

// Question label and option ids of the protected-paths question.
const (
	ProtectedPathsQuestionLabel    = "protected_paths"
	ProtectedPathsOptionApprove    = "approve"
	ProtectedPathsOptionReject     = "reject"
	ProtectedPathsOptionChangeSpec = "change_spec"
	protectedPathsQuestionDefault  = "hold"
)

const (
	// protectedPathsQuestionFiles caps the paths listed in the question text.
	protectedPathsQuestionFiles = 20
	// protectedPathsErrorFiles caps the paths named in Task.Error.
	protectedPathsErrorFiles = 5
)

// sortedProtectedPaths returns a sorted copy of paths.
func sortedProtectedPaths(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}

// protectedPathsQuestionText is the question the operator reads. It names the
// Automaton and the task, lists the changed protected paths (at most 20) and
// says in one sentence what each answer does.
func protectedPathsQuestionText(prd *PRD, t *Task, paths []string) string {
	var head strings.Builder
	fmt.Fprintf(&head, "Task %q (%s) of Automaton %q (%s) changed protected paths in its commit.\n\nProtected paths changed:",
		t.Title, t.ID, prd.Title, prd.ID)
	sorted := sortedProtectedPaths(paths)
	for i, p := range sorted {
		if i == protectedPathsQuestionFiles {
			fmt.Fprintf(&head, "\n- and %d more", len(sorted)-protectedPathsQuestionFiles)
			break
		}
		fmt.Fprintf(&head, "\n- %s", p)
	}
	tail := "\n\nApprove keeps the change and the task completes; the approval covers only these paths for this task." +
		" Reject puts the listed paths back to their state before the task, in a new commit, and runs the task again." +
		" Change the spec keeps the task blocked so you can edit its instructions, then reset it." +
		"\n\nNothing changes until you answer."
	text := head.String()
	if room := questionTextMaxBytes - len(tail); len(text) > room {
		if room < 0 {
			room = 0
		}
		text = text[:room]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text + tail
}

// protectedPathsQuestion builds the operator question for a held task. The
// gate has no basis to prefer one answer, so it carries no recommendation.
func protectedPathsQuestion(prd *PRD, t *Task, paths []string, baseCommit string) Question {
	_ = baseCommit
	return Question{
		Label:     ProtectedPathsQuestionLabel,
		AskedBy:   "autonomous",
		AskerKind: QuestionAskerSystem,
		PRDID:     prd.ID,
		TaskID:    t.ID,
		Context:   QuestionContextTask,
		Text:      protectedPathsQuestionText(prd, t, paths),
		Default:   protectedPathsQuestionDefault,
		Options: []QuestionOption{
			{ID: ProtectedPathsOptionApprove, Label: "Approve this change", Tradeoffs: "The task keeps the change and completes; the approval covers only these paths for this task."},
			{ID: ProtectedPathsOptionReject, Label: "Reject it", Tradeoffs: "The listed paths are put back to their state before the task, in a new commit, and the task runs again."},
			{ID: ProtectedPathsOptionChangeSpec, Label: "Change the spec", Tradeoffs: "The task stays blocked so you can edit its instructions, then reset it."},
		},
	}
}

// raiseProtectedPathsQuestion raises the question through the store the
// decision gate holds and returns its id. With no store, or when raising
// fails, it returns "" and the hold stands without a question.
func (m *Manager) raiseProtectedPathsQuestion(prd *PRD, t *Task, paths []string, baseCommit string) string {
	store, _, _ := m.decisionGateSettings()
	if store == nil {
		return ""
	}
	q, err := store.Raise(protectedPathsQuestion(prd, t, paths, baseCommit))
	if err != nil {
		log.Printf("[autonomous] prd=%s task=%s protected-paths: raise question: %v", prd.ID, t.ID, err)
		return ""
	}
	return q.ID
}

// recordProtectedPathsEvent audits and alerts one protected-paths event, both
// nil-safe.
func (m *Manager) recordProtectedPathsEvent(prd *PRD, t *Task, paths []string, baseCommit, action, title string) {
	m.mu.Lock()
	auditFn := m.auditFn
	alertFn := m.alertSink
	m.mu.Unlock()
	if auditFn != nil {
		auditFn(action, map[string]any{
			"prd_id":      prd.ID,
			"task_id":     t.ID,
			"paths":       paths,
			"base_commit": baseCommit,
		})
	}
	if alertFn != nil {
		body := "Automaton " + prd.ID + " (" + prd.Title + "), task " + t.ID + " (" + t.Title + "): the commit changed protected paths: " +
			boundedFileList(paths, protectedPathsQuestionFiles)
		alertFn("warn", title, body)
	}
}

// protectedPathsNote is the one-line Decision note.
func protectedPathsNote(t *Task, paths []string) string {
	return "task " + t.ID + ": protected paths changed: " + boundedFileList(paths, protectedPathsQuestionFiles)
}

// holdForProtectedPaths parks t in TaskBlocked because its commit changed
// protected paths. It records the hold on the task and the Automaton decision
// log, audits and alerts, raises one question (default hold) and saves and
// announces the status. A task that already carries a hold whose question is
// still open is left alone: no second question is raised. With no question
// store the task is still held and QuestionID stays empty. A held task
// consumes no auto-fix retry budget and records no failed attempt.
func (m *Manager) holdForProtectedPaths(prd *PRD, t *Task, paths []string, baseCommit string) error {
	var existing string
	m.store.WithLock(func() {
		if t.ProtectedPathHold != nil {
			existing = t.ProtectedPathHold.QuestionID
		}
	})
	if m.openUncommittedQuestion(existing) {
		return nil
	}

	sorted := sortedProtectedPaths(paths)
	questionID := m.raiseProtectedPathsQuestion(prd, t, sorted, baseCommit)
	note := protectedPathsNote(t, sorted)

	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.Status = TaskBlocked
		t.Error = "protected_path: " + boundedFileList(sorted, protectedPathsErrorFiles)
		t.ProtectedPathHold = &ProtectedPathHold{
			Paths:      sorted,
			Action:     config.ProtectedPathsActionHold,
			Reason:     "the task's commit changed protected paths",
			QuestionID: questionID,
			BaseCommit: baseCommit,
			Since:      time.Now(),
		}
	})
	m.appendUncommittedDecision(prd, protectedPathsHeldDecisionKind, note, questionID)
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save Automaton: %v", prd.ID, t.ID, err)
	}
	m.recordProtectedPathsEvent(prd, t, sorted, baseCommit, protectedPathsHeldAuditAction, "Protected paths held")
	log.Printf("[autonomous] WARNING %s/%s held: protected paths changed: %s", prd.ID, t.ID, boundedFileList(sorted, protectedPathsErrorFiles))
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	return nil
}

// warnProtectedPaths is the warn action: it records the protected_paths_warned
// decision, audits and raises a warn alert. It changes nothing on the task and
// asks no question.
func (m *Manager) warnProtectedPaths(prd *PRD, t *Task, paths []string, baseCommit string) {
	sorted := sortedProtectedPaths(paths)
	m.appendUncommittedDecision(prd, protectedPathsWarnedDecisionKind, protectedPathsNote(t, sorted), "")
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save Automaton: %v", prd.ID, t.ID, err)
	}
	m.recordProtectedPathsEvent(prd, t, sorted, baseCommit, protectedPathsWarnedAuditAction, "Protected paths warning")
	log.Printf("[autonomous] WARNING %s/%s protected paths changed: %s", prd.ID, t.ID, boundedFileList(sorted, protectedPathsErrorFiles))
}

// blockProtectedPaths is the block action: it parks t in TaskBlocked with the
// changed paths in Task.Error, records the decision, audits and alerts, and
// raises no question. The operator authorises the path with the Automaton's
// authorised paths, or edits the task, then resets it.
func (m *Manager) blockProtectedPaths(prd *PRD, t *Task, paths []string, baseCommit string) error {
	sorted := sortedProtectedPaths(paths)
	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.Status = TaskBlocked
		t.Error = "protected_path_blocked: " + boundedFileList(sorted, protectedPathsErrorFiles)
	})
	m.appendUncommittedDecision(prd, protectedPathsBlockedDecisionKind, protectedPathsNote(t, sorted), "")
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s/%s protected-paths: save Automaton: %v", prd.ID, t.ID, err)
	}
	m.recordProtectedPathsEvent(prd, t, sorted, baseCommit, protectedPathsBlockedAuditAction, "Protected paths blocked")
	log.Printf("[autonomous] WARNING %s/%s blocked: protected paths changed: %s", prd.ID, t.ID, boundedFileList(sorted, protectedPathsErrorFiles))
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	return nil
}
