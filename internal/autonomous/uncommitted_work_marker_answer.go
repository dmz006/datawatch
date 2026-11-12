// Marker-only uncommitted-work questions.
//
// Before the uncommitted-work check learned to skip the daemon's own marker
// files, a hold could be raised for a worktree whose only "uncommitted" file was
// DATAWATCH_COMPLETE. Such a question can never be answered usefully by
// committing or discarding it. The sweep here answers those old questions with
// the ignore option once, at daemon start, so the Automaton runs on.
package autonomous

import (
	"fmt"
	"log"
	"strings"
)

// markerOnlyIgnoreNote is the decision-log line for an auto-ignored question.
const markerOnlyIgnoreNote = "ignored: the question listed only the daemon marker file(s) %s, which are not work to commit"

// markerOnlyHoldFiles reports whether files is non-empty and every entry is a
// daemon marker path.
func markerOnlyHoldFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !isDaemonMarkerPath(f) {
			return false
		}
	}
	return true
}

// withIgnoreOption returns opts with the ignore option appended when it is not
// already listed, and whether it was added.
//
// The question store only accepts an answer whose option id is listed on the
// question (CheckAnswerAllowed), and questions raised before the ignore option
// existed do not list it. New questions are left exactly as they were: the
// option is added only here, to an open question whose hold lists marker files
// and nothing else, through the store's own Update path.
func withIgnoreOption(opts []QuestionOption) ([]QuestionOption, bool) {
	for _, o := range opts {
		if o.ID == UncommittedOptionIgnore {
			return opts, false
		}
	}
	out := append([]QuestionOption(nil), opts...)
	out = append(out, QuestionOption{
		ID:        UncommittedOptionIgnore,
		Label:     "Ignore (marker files only)",
		Tradeoffs: "The question lists only the daemon's own marker files. Nothing is committed, discarded or deleted; the Automaton runs on.",
	})
	return out, true
}

// sweepMarkerOnlyUncommittedQuestions answers every open uncommitted-work
// question whose hold lists only daemon marker files with the ignore option,
// as actor "autonomous", and logs a decision on the Automaton. A question that
// lists any real file is left alone. It never removes a file and never signals
// or stops a session, is idempotent, and does nothing without a question store.
// It returns how many questions it answered.
func (m *Manager) sweepMarkerOnlyUncommittedQuestions() int {
	store, _, _ := m.decisionGateSettings()
	if store == nil || m.store == nil {
		return 0
	}
	answered := 0
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Limit: 1000}) {
		if q.Label != UncommittedQuestionLabel || q.State != QuestionOpen || q.PRDID == "" {
			continue
		}
		prd, ok := m.store.GetPRD(q.PRDID)
		if !ok {
			continue
		}
		var hold *UncommittedWorkHold
		var files []string
		m.store.WithLock(func() {
			if q.TaskID != "" {
				if t, _ := findTaskInPRD(prd, q.TaskID); t != nil {
					hold = t.UncommittedWork
				}
			} else {
				hold = prd.UncommittedWork
			}
			if hold != nil && hold.QuestionID == q.ID {
				files = append([]string(nil), hold.Files...)
			} else {
				hold = nil
			}
		})
		if hold == nil || !markerOnlyHoldFiles(files) {
			continue
		}
		if opts, added := withIgnoreOption(q.Options); added {
			if _, err := store.Update(q.ID, QuestionPatch{Options: opts}, "autonomous"); err != nil {
				log.Printf("[autonomous] %s uncommitted-work marker sweep: add ignore option: %v", prd.ID, err)
				continue
			}
		}
		ans, err := store.Answer(q.ID, Answer{
			ChosenOptionID: UncommittedOptionIgnore,
			Actor:          "autonomous",
			Surface:        "daemon",
		})
		if err != nil {
			log.Printf("[autonomous] %s uncommitted-work marker sweep: answer: %v", prd.ID, err)
			continue
		}
		answered++
		m.appendUncommittedDecision(prd, uncommittedResolvedDecisionKind,
			fmt.Sprintf(markerOnlyIgnoreNote, strings.Join(files, ", ")), q.ID)
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] %s uncommitted-work marker sweep: save Automaton: %v", prd.ID, err)
		}
		// The store's change hook acts on the answer when the Automaton API is
		// already wired. At daemon start it may not be, so act on it here too;
		// the second call finds the hold cleared (stale) and changes nothing.
		answeredQ, _ := store.Get(q.ID)
		m.resolveUncommittedWork(QuestionChange{Kind: QuestionChangeAnswered, Question: answeredQ, Answer: &ans})
	}
	return answered
}

// SweepMarkerOnlyUncommittedQuestions is the daemon-start entry point for
// sweepMarkerOnlyUncommittedQuestions.
func (m *Manager) SweepMarkerOnlyUncommittedQuestions() int {
	return m.sweepMarkerOnlyUncommittedQuestions()
}
