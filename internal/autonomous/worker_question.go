// Worker questions: raising a recognised DATAWATCH_QUESTION: marker line into
// the operator question store (the Step 4 operator decision channel). The
// session package's output scanner recognises the line and hands this file an
// event; the question then takes exactly the path a POST /api/questions body
// takes, so its audit entry, decision-log entry and raise notifications are
// the ones a REST raise produces. A refused line is never raised: it is
// audited by session id, reason category and line hash, and logged once.
package autonomous

import (
	"errors"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

// Worker question constants.
const (
	// workerAskerPrefix starts the asked_by of a worker question; the short
	// session id follows it.
	workerAskerPrefix = "worker-"
	// workerQuestionDefaultHold is the default of a marker that names none.
	workerQuestionDefaultHold = "hold"
	// workerQuestionReasonInvalidDefault and workerQuestionReasonInvalid are
	// refusal reason categories this side adds to the scanner's own.
	workerQuestionReasonInvalidDefault = "invalid_default"
	workerQuestionReasonInvalid        = "invalid_question"
	// workerQuestionReasonUnknown labels a refusal event that carries no reason.
	workerQuestionReasonUnknown = "unknown"
)

// WorkerQuestionDeps is what RaiseWorkerQuestion needs. Store is required.
// Audit, Notify and Logf may each be nil.
type WorkerQuestionDeps struct {
	// Store is the question store a REST raise uses.
	Store *QuestionStore
	// Audit receives the question_marker_refused entry.
	Audit func(action string, details map[string]any)
	// Notify is the raise fan-out a REST raise runs after its response
	// (WebSocket event, messaging group, high-priority alert). It is called
	// once per question actually raised, never for a duplicate.
	Notify func(Question)
	// Logf receives the one log line of a refusal or a store fault.
	Logf func(format string, args ...any)
}

func (d WorkerQuestionDeps) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

// RaiseWorkerQuestion raises the question a worker session asked with the
// DATAWATCH_QUESTION: marker. It returns the stored question and true when a
// new question was raised. It returns false, with the existing question where
// there is one, when the line was a duplicate of an open question, was
// refused, or could not be stored; none of those is an error the caller must
// handle, each is audited or logged here.
//
// The question is asked_by "worker-" plus the short session id, asker_kind
// session, linked to the session, and to its task when the event names one.
// The context is task when a task id is set. With no task id it is prd when a
// PRD id is set (a none context may not carry a link, and the question must
// still land on that PRD's decision log) and none otherwise. It never
// returns to the session (return_path false) and its priority is normal. The
// text, options, recommendation, default (absent means hold), blocking, label
// and risk come from the marker. ValidateNewQuestion stays the authority; a
// question it refuses is treated as a refusal.
func RaiseWorkerQuestion(d WorkerQuestionDeps, ev session.WorkerQuestionEvent) (Question, bool) {
	if ev.Signal == nil {
		d.refuse(ev, ev.RefusalReason)
		return Question{}, false
	}
	sig := ev.Signal

	def := sig.Default
	if def == "" {
		def = workerQuestionDefaultHold
	}
	if def != workerQuestionDefaultHold && !signalHasOption(sig, def) {
		d.refuse(ev, workerQuestionReasonInvalidDefault)
		return Question{}, false
	}

	q := Question{
		Label:            sig.Label,
		AskedBy:          workerAskerPrefix + sessionShortID(ev.SessionFullID),
		AskerKind:        QuestionAskerSession,
		SessionID:        ev.SessionFullID,
		Context:          workerQuestionContext(ev),
		PRDID:            ev.PRDID,
		TaskID:           ev.TaskID,
		Text:             sig.Question,
		Default:          def,
		Risk:             sig.Risk,
		Priority:         QuestionPriorityNormal,
		ReturnPath:       false,
		Blocking:         sig.Blocked,
		Source:           QuestionSourceMarker,
		SourceLineSHA256: workerLineHash(ev),
	}
	for _, o := range sig.Options {
		q.Options = append(q.Options, QuestionOption{ID: o.ID, Label: o.Label, Tradeoffs: o.Tradeoffs})
	}
	if sig.Recommendation != nil {
		q.Recommendation = &QuestionRecommendation{
			OptionID:  sig.Recommendation.OptionID,
			Rationale: sig.Recommendation.Rationale,
		}
	}

	if d.Store == nil {
		d.logf("[autonomous] worker question from session %s not raised: no question store", ev.SessionFullID)
		return Question{}, false
	}
	raised, isNew, err := d.Store.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		if errors.Is(err, ErrInvalidQuestion) {
			d.refuse(ev, workerQuestionReasonInvalid)
		} else {
			d.logf("[autonomous] worker question from session %s not stored: %v", ev.SessionFullID, err)
		}
		return Question{}, false
	}
	if !isNew {
		// The same pane line read again while its question is still open.
		recordWorkerQuestion(WorkerQuestionOutcomeDuplicate)
		return raised, false
	}
	recordWorkerQuestion(WorkerQuestionOutcomeRaised)
	if d.Notify != nil {
		d.Notify(raised)
	}
	return raised, true
}

// refuse audits and logs a refused marker line. The audit entry carries the
// session id, the reason category and the line hash, never the line text.
func (d WorkerQuestionDeps) refuse(ev session.WorkerQuestionEvent, reason string) {
	if reason == "" {
		reason = workerQuestionReasonUnknown
	}
	hash := workerLineHash(ev)
	recordWorkerQuestion(WorkerQuestionOutcomeRefused)
	if d.Audit != nil {
		d.Audit(audit.ActionQuestionMarkerRefused, map[string]any{
			"session_id":  ev.SessionFullID,
			"reason":      reason,
			"line_sha256": hash,
		})
	}
	d.logf("[autonomous] worker question marker refused: session=%s reason=%s line_sha256=%s",
		ev.SessionFullID, reason, hash)
}

// workerLineHash is the hash of the marker line the event came from.
func workerLineHash(ev session.WorkerQuestionEvent) string {
	if ev.LineSHA256 != "" {
		return ev.LineSHA256
	}
	if ev.Signal != nil {
		return ev.Signal.LineSHA256
	}
	return ""
}

// workerQuestionContext picks the context kind for a worker question.
func workerQuestionContext(ev session.WorkerQuestionEvent) string {
	switch {
	case ev.TaskID != "":
		return QuestionContextTask
	case ev.PRDID != "":
		return QuestionContextPrd
	default:
		return QuestionContextNone
	}
}

// signalHasOption reports whether the signal has an option with this id.
func signalHasOption(sig *session.WorkerQuestionSignal, id string) bool {
	for _, o := range sig.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}
