// Attempt cap — seeding the count of tasks that ran before the cap existed.
//
// Task.AttemptsTotal counts the workers launched for a task from the day the
// cap shipped; a task from before that day starts at zero although its history
// (Task.Attempts) shows it ran many times. SeedAttemptCounts runs once, at the
// first start after the upgrade: it sets every task's count to the number of
// attempts recorded for it (completed tasks too, so the record is accurate),
// holds every unfinished task that is over the cap, and raises ONE operator
// question per Automaton that lists the tasks it holds, instead of one per task.
// Completed tasks are counted but never held. The sweep is one-shot (a marker
// file in the data directory), so an operator's force reset, which sets the
// count back to zero and keeps the history, is not undone by a later start.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// attemptCapSeedMarker is the file in the data directory that records the
	// sweep has run.
	attemptCapSeedMarker = "attempt-count-seeded"
	// attemptCapSeedQuestionAsker is the asked_by of the grouped question.
	attemptCapSeedQuestionAsker = "attempt-cap-seed"
	// attemptCapSeedAlertTitle is the title of the one alert per Automaton.
	attemptCapSeedAlertTitle = "Automaton tasks held at the attempt cap"
	// attemptCapActionKeepHeld is the question option that leaves the tasks held.
	attemptCapActionKeepHeld = "keep_held"
	// attemptCapSeedListMax is how many held tasks the question text names.
	attemptCapSeedListMax = 20
)

// AttemptSeedReport is what one seeding sweep did.
type AttemptSeedReport struct {
	// Ran is false when the sweep had already run and did nothing.
	Ran bool `json:"ran"`
	// Seeded is the number of tasks whose count was raised to their history.
	Seeded int `json:"seeded"`
	// HeldTasks and HeldAutomata are the tasks held at the cap and the
	// Automata they belong to (one question each).
	HeldTasks    int `json:"held_tasks"`
	HeldAutomata int `json:"held_automata"`
}

// seedHeldTask is one task the sweep held, for the grouped question.
type seedHeldTask struct {
	id, title string
	total     int
}

// attemptSeedHoldable reports whether the sweep holds a task in this status. A
// completed or cancelled task is finished. A task in progress, running its
// tests or being verified has a live worker or verifier that the next attempt
// gate holds instead of the sweep, which would orphan it.
func attemptSeedHoldable(t *Task) bool {
	switch t.Status {
	case TaskCompleted, TaskCancelled, TaskInProgress, TaskRunningTests, TaskVerifying:
		return false
	case TaskReviewNeeded:
		return !t.AttemptCapHeld
	}
	return true
}

// attemptSeedPRDActive reports whether an Automaton in this status can start
// tasks, so a task held at the cap would otherwise run. A held, finished,
// cancelled, archived or not yet approved Automaton is left alone: its tasks
// meet the cap at the attempt gate if it ever runs.
func attemptSeedPRDActive(s PRDStatus) bool {
	switch s {
	case PRDApproved, PRDRunning, PRDActive, PRDPaused, PRDBlocked, PRDFailed:
		return true
	}
	return false
}

// SeedAttemptCounts runs the one-time sweep described in the file comment and
// reports what it did. It is safe to call on every start: after the first run
// it returns at once. A failure to hold one task is logged and the sweep goes
// on; the marker is written when the sweep ends.
func (m *Manager) SeedAttemptCounts() AttemptSeedReport {
	marker := ""
	if m.dataDir != "" {
		marker = filepath.Join(m.dataDir, attemptCapSeedMarker)
		if _, err := os.Stat(marker); err == nil {
			return AttemptSeedReport{}
		}
	}
	rep := AttemptSeedReport{Ran: true}
	for _, prd := range m.store.ListPRDs() {
		var tasks []*Task
		var status PRDStatus
		m.store.WithLock(func() {
			status = prd.Status
			for si := range prd.Story {
				for ti := range prd.Story[si].Tasks {
					tasks = append(tasks, &prd.Story[si].Tasks[ti])
				}
			}
		})
		var candidates []*Task
		for _, t := range tasks {
			changed := false
			m.store.WithLock(func() {
				if n := len(t.Attempts); n > t.AttemptsTotal {
					t.AttemptsTotal = n
					changed = true
				}
			})
			if changed {
				rep.Seeded++
				if err := m.store.SaveTask(t); err != nil {
					log.Printf("[autonomous] prd=%s task=%s: save seeded attempt count: %v", prd.ID, t.ID, err)
				}
			}
			var holdable bool
			m.store.WithLock(func() { holdable = attemptSeedHoldable(t) })
			if holdable && attemptSeedPRDActive(status) && m.attemptCapReached(prd, t) {
				candidates = append(candidates, t)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		var held []seedHeldTask
		for _, t := range candidates {
			var total int
			var id, title string
			m.store.WithLock(func() { total, id, title = t.AttemptsTotal, t.ID, t.Title })
			_, maxTotal := m.attemptCapConfig(prd)
			detail := fmt.Sprintf("%d attempts are recorded in the task's history, the cap is %d", total, maxTotal)
			note := fmt.Sprintf("task %s: %d recorded attempts counted at the first start with the attempt cap, over the cap of %d; held with the other tasks of the Automaton in one question",
				id, total, maxTotal)
			if _, _, _, err := m.parkAtAttemptCap(prd, t, detail, note); err != nil {
				log.Printf("[autonomous] prd=%s task=%s: hold at the attempt cap after seeding: %v", prd.ID, id, err)
				continue
			}
			held = append(held, seedHeldTask{id: id, title: title, total: total})
		}
		if len(held) == 0 {
			continue
		}
		rep.HeldTasks += len(held)
		rep.HeldAutomata++
		m.raiseAttemptCapSeedAlert(prd, held)
		m.raiseAttemptCapSeedQuestion(prd, held)
	}
	if marker != "" {
		stamp := fmt.Sprintf("seeded %d, held %d task(s) in %d Automaton(s) at %s\n",
			rep.Seeded, rep.HeldTasks, rep.HeldAutomata, m.attemptNow().Format(time.RFC3339))
		if err := os.WriteFile(marker, []byte(stamp), 0o644); err != nil {
			log.Printf("[autonomous] write %s: %v", marker, err)
		}
	}
	log.Printf("[autonomous] attempt cap: seeded %d task count(s) from history, held %d task(s) in %d Automaton(s)",
		rep.Seeded, rep.HeldTasks, rep.HeldAutomata)
	return rep
}

// seedHeldList renders the held tasks, most attempts first, capped at
// attemptCapSeedListMax with a count of the rest.
func seedHeldList(held []seedHeldTask) string {
	sorted := append([]seedHeldTask(nil), held...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].total > sorted[j].total })
	var parts []string
	for i, h := range sorted {
		if i == attemptCapSeedListMax {
			parts = append(parts, fmt.Sprintf("and %d more", len(sorted)-attemptCapSeedListMax))
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s, %d attempts)", h.title, h.id, h.total))
	}
	return strings.Join(parts, "; ")
}

// raiseAttemptCapSeedAlert sends the one alert for an Automaton whose tasks the
// sweep held. Nothing is sent when no alert sink is wired.
func (m *Manager) raiseAttemptCapSeedAlert(prd *PRD, held []seedHeldTask) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	_, maxTotal := m.attemptCapConfig(prd)
	alert("error", attemptCapSeedAlertTitle, fmt.Sprintf(
		"Automaton: %s (%s)\nTasks held: %d (attempt cap %d): %s\n"+
			"These tasks ran before the cap existed and their history already shows more attempts than the cap allows. "+
			"Nothing starts them again until you answer the operator question for the Automaton, or force-reset a task.",
		prd.ID, prd.Title, len(held), maxTotal, seedHeldList(held)))
}

// attemptCapSeedQuestionKey is the dedupe key of the grouped question of one
// Automaton.
func attemptCapSeedQuestionKey(prdID string) string {
	sum := sha256.Sum256([]byte("attempt-cap-seed\x00" + prdID))
	return hex.EncodeToString(sum[:])
}

// raiseAttemptCapSeedQuestion raises the ONE operator question of an Automaton
// whose tasks the sweep held: clear the counts of all of them and run them
// again, or keep them held. The default is to keep them held. Nothing is raised
// when no question store is wired or the operator question channel is off; the
// alert and each task's report carry the facts then.
func (m *Manager) raiseAttemptCapSeedQuestion(prd *PRD, held []seedHeldTask) {
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return
	}
	_, maxTotal := m.attemptCapConfig(prd)
	q := Question{
		Label:     infraHoldQuestionLabel,
		AskedBy:   attemptCapSeedQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextPrd,
		PRDID:     prd.ID,
		Text: fmt.Sprintf("%d task(s) in Automaton %s are over the attempt cap of %d, counted from their recorded history: %s. Clear their counts and run them again, or keep them held?",
			len(held), automatonLabel(prd), maxTotal, seedHeldList(held)),
		Options: []QuestionOption{
			{ID: attemptCapActionForceReset, Label: "Clear the counts and run them again",
				Tradeoffs: "Force-resets every task listed as the operator: each count goes back to zero and each task runs a fresh set of attempts."},
			{ID: attemptCapActionKeepHeld, Label: "Keep them held",
				Tradeoffs: "Leaves the tasks held. You can still force-reset any one of them later."},
		},
		Default:          attemptCapActionKeepHeld,
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: attemptCapSeedQuestionKey(prd.ID),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s: raise attempt cap seed question: %v", prd.ID, err)
	}
}

// capHeldTaskIDs returns the ids of the tasks of prd that are held at the
// attempt cap.
func (m *Manager) capHeldTaskIDs(prd *PRD) []string {
	var ids []string
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.AttemptCapHeld && t.Status == TaskReviewNeeded {
					ids = append(ids, t.ID)
				}
			}
		}
	})
	return ids
}

// withdrawAttemptCapSeedQuestion withdraws the open grouped question of an
// Automaton once none of its tasks is held at the cap any more (each was
// force-reset or cancelled one by one), so no question waits for a decision that
// no longer matters.
func (m *Manager) withdrawAttemptCapSeedQuestion(prdID, actor, reason string) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok || len(m.capHeldTaskIDs(prd)) > 0 {
		return
	}
	if actor == "" {
		actor = infraHoldWithdrawActor
	}
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapSeedQuestionAsker, PRDID: prdID, Limit: 100}) {
		if _, err := store.Withdraw(q.ID, actor, reason); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
			log.Printf("[autonomous] prd=%s: withdraw attempt cap seed question %s: %v", prdID, q.ID, err)
		}
	}
}

// resolveAttemptCapSeedAnswer acts on an answered grouped question: the
// operator's clear answer force-resets every task of the Automaton that is held
// at the cap and runs it; the same answer from any other caller clears nothing
// and is noted; keeping them held changes nothing. It reports whether the
// change was such an answer.
func (a *API) resolveAttemptCapSeedAnswer(c QuestionChange) bool {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil || q.AskedBy != attemptCapSeedQuestionAsker ||
		q.AskerKind != QuestionAskerSystem || q.PRDID == "" {
		return false
	}
	actor := c.Answer.Actor
	if actor == "" {
		actor = reviewDecisionActor
	}
	if c.Answer.ChosenOptionID != attemptCapActionForceReset {
		return true
	}
	if !isOperatorActor(actor) {
		a.M.noteAttemptCapSeedAnswerIgnored(q.PRDID, actor, q.ID)
		return true
	}
	prd, ok := a.M.store.GetPRD(q.PRDID)
	if !ok {
		return true
	}
	cleared := 0
	for _, id := range a.M.capHeldTaskIDs(prd) {
		if _, err := a.M.ResetTaskWithOptions(q.PRDID, id, actor, true, false); err != nil {
			log.Printf("[autonomous] prd=%s task=%s: force reset after question %s answered: %v", q.PRDID, id, q.ID, err)
			continue
		}
		cleared++
	}
	if cleared > 0 {
		_ = a.Run(q.PRDID)
	}
	return true
}

// noteAttemptCapSeedAnswerIgnored records on the Automaton that a clear answer
// to the grouped question came from a caller that is not the operator.
func (m *Manager) noteAttemptCapSeedAnswerIgnored(prdID, actor, questionID string) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: m.attemptNow(), Kind: attemptCapAnswerIgnoredDecisionKind, Actor: actor,
			Note: fmt.Sprintf("the clear answer to question %s came from %q, not the operator; the attempt counts are kept", questionID, actor),
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s save attempt cap seed answer note: %v", prdID, err)
	}
}
