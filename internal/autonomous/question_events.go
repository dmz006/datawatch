// question_events.go
//
// Operator question lifecycle events: question.answered (an open question
// gained an answer — first or superseding) and question.withdrawn (an open
// question was withdrawn). Both reuse the in-process lifecycle dispatcher and
// the engine's per-subscriber queue exactly as the task.looping event does;
// there is one emit path and one queue for every kind, not a second queue
// for questions.
//
// Emit path (exactly once per answer / per withdrawal):
//  1. the store appends the answer/withdrawal record (durable);
//  2. the store fires the event through its sink — the daemon wires that to
//     Manager.EmitQuestionEvent below, gated on the
//     autonomous.questions.enabled switch;
//  3. the store appends one question_event_emitted marker line to the same
//     append-only file. The marker is the store's proof the event entered
//     the queue: replay records it, and a startup reconcile
//     (ReconcileEmittedEvents) re-fires exactly the answered and withdrawn
//     records that have no marker — the window between the answer write and
//     the enqueue in which a crash otherwise loses the event.
package autonomous

import (
	"sort"
	"strings"
)

// QuestionEventSink hands a fully built question lifecycle event to the
// store's emit path. The daemon wires it to Manager.EmitQuestionEvent (and
// the switch off means it is nil: no event is fired at all). A nil sink
// disables emission.
type QuestionEventSink func(ev LifecycleEvent)

// questionEventDetailsMax caps one Details line so the marker's record and
// every subscriber's log line stay a bounded single line.
const questionEventDetailsMax = 512

// buildQuestionEvent base copies the question's identity onto an event: the
// level is task when the question names a task (the finest scope it
// touches), else PRD — the question is an Automaton-level operator
// decision; the kind names it either way.
func buildQuestionEventBase(q *Question) LifecycleEvent {
	ev := LifecycleEvent{Level: LevelPRD}
	if q == nil {
		return ev
	}
	if q.TaskID != "" {
		ev.Level = LevelTask
	}
	ev.QuestionID = q.ID
	ev.PRDID = q.PRDID
	ev.TaskID = q.TaskID
	ev.SessionID = q.SessionID
	return ev
}

// questionEventDetails joins the parts into one bounded line; every part is
// a single-line store field, so nothing in the result carries a newline.
func questionEventDetails(parts ...string) string {
	s := strings.Join(parts, ", ")
	if len(s) > questionEventDetailsMax {
		s = s[:questionEventDetailsMax] + "…"
	}
	return s
}

// BuildQuestionAnsweredEvent builds the question.answered event for the
// answer a on question q: the kind, the question, the answer record it
// names, and a one-line description.
func BuildQuestionAnsweredEvent(q *Question, a *Answer) LifecycleEvent {
	ev := buildQuestionEventBase(q)
	ev.Kind = KindQuestionAnswered
	if a != nil {
		ev.AnswerID = a.ID
	}
	parts := []string{"question " + safeQuestionID(q) + " answered"}
	if a != nil {
		if a.ChosenOptionID != "" {
			parts = append(parts, "option "+a.ChosenOptionID)
		}
		if a.Supersedes != "" {
			parts = append(parts, "supersedes "+a.Supersedes)
		}
		if a.Actor != "" {
			parts = append(parts, "by "+a.Actor)
		}
	}
	ev.Details = questionEventDetails(parts...)
	return ev
}

// BuildQuestionWithdrawnEvent builds the question.withdrawn event for the
// withdrawal of question q by actor with reason. Both may be empty.
func BuildQuestionWithdrawnEvent(q *Question, actor, reason string) LifecycleEvent {
	ev := buildQuestionEventBase(q)
	ev.Kind = KindQuestionWithdrawn
	parts := []string{"question " + safeQuestionID(q) + " withdrawn"}
	if actor != "" {
		parts = append(parts, "by "+strings.ReplaceAll(actor, "\n", " "))
	}
	if reason != "" {
		parts = append(parts, "reason: "+strings.ReplaceAll(reason, "\n", " "))
	}
	ev.Details = questionEventDetails(parts...)
	return ev
}

// safeQuestionID is the event's question name, empty-safe.
func safeQuestionID(q *Question) string {
	if q == nil {
		return ""
	}
	return q.ID
}

// EmitQuestionEvent hands ev to the manager's lifecycle dispatcher — the
// same bus, the engine's per-subscriber queue and the same subscriber
// routing as loop_rescue.go's task.looping events. It stamps the ID, At and
// Host and never blocks; it is a no-op on a nil manager.
func (m *Manager) EmitQuestionEvent(ev LifecycleEvent) {
	if m == nil {
		return
	}
	m.emit(ev)
}

// SetEventSink wires the store's question event sink (see
// QuestionEventSink); a nil fn disables emission — the answer and the
// withdrawal records are still written, only the event is not. The sink is
// invoked outside the store lock, after the record append.
func (s *QuestionStore) SetEventSink(fn QuestionEventSink) {
	s.mu.Lock()
	s.eventSink = fn
	s.mu.Unlock()
}

// fireQuestionEvent invokes the sink, if wired, outside the store lock and
// reports whether the event was fired (false when no sink is set — the
// daemon's switch-off: no event, and therefore no marker and no claim of
// emission either).
func (s *QuestionStore) fireQuestionEvent(ev LifecycleEvent) bool {
	s.mu.Lock()
	fn := s.eventSink
	s.mu.Unlock()
	if fn == nil {
		return false
	}
	fn(ev)
	return true
}

// MarkEventEmitted appends the event's question_event_emitted marker line
// and records it in memory. Call it only after the event is in the queue.
// It returns false when the marker could not be written; the store is left
// consistent either way (the answer/withdrawal record stands; a later
// reconcile may still fire the event).
func (s *QuestionStore) MarkEventEmitted(ev LifecycleEvent) bool {
	now := s.now()
	rec := questionStoreRecord{
		Type:       storeLineQuestionEventEmitted,
		QuestionID: ev.QuestionID,
		AnswerID:   ev.AnswerID,
		EventKind:  ev.Kind,
		At:         &now,
	}
	s.mu.Lock()
	if err := s.appendLocked(rec); err != nil {
		s.mu.Unlock()
		return false
	}
	s.eventEmitted[questionEventKey(ev.QuestionID, ev.Kind, ev.AnswerID)] = true
	s.mu.Unlock()
	return true
}

// ReconcileEmittedEvents lists the question lifecycle events the store
// holds the record for but has not marked emitted yet: one
// question.answered per answered question (for its current answer) and one
// question.withdrawn per withdrawn question, ordered by question id. This is
// the daemon-start repair: replay has rebuilt the view, and whatever lacks
// its marker — a crash in the window between the answer (or withdrawal)
// write and the enqueue — fires once here and once only, then it is marked.
func (s *QuestionStore) ReconcileEmittedEvents() []LifecycleEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	type pending struct {
		questionID string
		ev         LifecycleEvent
	}
	var out []pending
	for id, q := range s.questions {
		switch q.State {
		case QuestionAnswered:
			ids := s.answerIDs[id]
			if len(ids) == 0 {
				continue
			}
			current := ids[len(ids)-1]
			if s.eventEmitted[questionEventKey(id, KindQuestionAnswered, current)] {
				continue
			}
			a, ok := s.answers[current]
			if !ok {
				continue
			}
			out = append(out, pending{questionID: id, ev: BuildQuestionAnsweredEvent(q, a)})
		case QuestionWithdrawn:
			if s.eventEmitted[questionEventKey(id, KindQuestionWithdrawn, "")] {
				continue
			}
			meta := s.withdrawMeta[id]
			out = append(out, pending{questionID: id, ev: BuildQuestionWithdrawnEvent(q, meta.actor, meta.reason)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].questionID < out[j].questionID })
	events := make([]LifecycleEvent, 0, len(out))
	for _, p := range out {
		events = append(events, p.ev)
	}
	return events
}

// questionEventKey names one marker uniquely: the question, the event kind
// and (for question.answered) the answer event named.
func questionEventKey(questionID, kind, answerID string) string {
	return questionID + "\x00" + kind + "\x00" + answerID
}
