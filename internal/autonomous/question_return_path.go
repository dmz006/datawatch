// question_return_path.go — the per-question opt-in session line on answer.
//
// When a question is raised with return_path true, an answer recorded
// against it is additionally delivered into the asking session as exactly
// one line of compact JSON with a fixed marker:
//
//	DATAWATCH_ANSWER: {"question_id":"...","answer_id":"...","chosen_option":"...","quote":"...","actor":"...","at":"..."}
//
// Marker conventions (same family as the session manager's DATAWATCH
// markers, internal/session/manager.go):
//   - The line is exactly one line: encoding/json marshals every control
//     byte of the verbatim quote as a JSON escape, so the payload itself
//     carries no raw newline.
//   - DATAWATCH_COMPLETE: and DATAWATCH_NEEDS_INPUT: are matched only as
//     the prefix of a CR-separated visual segment, and this line always
//     starts with "DATAWATCH_ANSWER: " — it can therefore never be read as
//     either of those markers, or re-fed into their paths.
//   - Readers split on the stable prefix: everything after "DATAWATCH_" +
//     the fixed marker is the JSON document, nothing else.
//
// Delivery never blocks an answer call and can never fail one: the answer
// is already recorded in the store, and the default return path (event
// plus polled GET) still carries it. A gone session or a failed send costs
// one warning line and nothing else; the sender is retried by no one.
package autonomous

import (
	"encoding/json"
	"time"
)

// AnswerMarker is the fixed marker of the per-question session line. It is
// distinct from, and not read as, DATAWATCH_COMPLETE: and
// DATAWATCH_NEEDS_INPUT:.
const AnswerMarker = "DATAWATCH_ANSWER:"

// AnswerMarkerPrefix is the marker plus exactly one space — the prefix of
// every delivered line, and the split point for the reader.
const AnswerMarkerPrefix = AnswerMarker + " "

// answerMarkerPayload is the compact JSON document the line carries. The
// keys are fixed: question_id, answer_id, chosen_option, quote (the
// operator's text exactly as recorded, escapes included in the wire form),
// actor, and at in RFC 3339 UTC.
type answerMarkerPayload struct {
	QuestionID   string `json:"question_id"`
	AnswerID     string `json:"answer_id"`
	ChosenOption string `json:"chosen_option"`
	Quote        string `json:"quote"`
	Actor        string `json:"actor"`
	AtUTC        string `json:"at"`
}

// BuildAnswerMarkerLine builds the one-line return path entry for one
// recorded answer: AnswerMarkerPrefix ("DATAWATCH_ANSWER: " — marker plus
// one space) followed by the compact JSON document. "at" is the answer's
// creation time in RFC 3339 UTC, or now when the record has none. The
// result never contains a raw newline.
func BuildAnswerMarkerLine(question Question, answer Answer) string {
	at := time.Now()
	if answer.CreatedAt != nil {
		at = *answer.CreatedAt
	}
	p := answerMarkerPayload{
		QuestionID:   question.ID,
		AnswerID:     answer.ID,
		ChosenOption: answer.ChosenOptionID,
		Quote:        answer.Quote,
		Actor:        answer.Actor,
		AtUTC:        at.UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(p)
	if err != nil {
		// Unreachable: every field is a plain string. Keep the line
		// well-formed either way.
		return AnswerMarkerPrefix + "{}"
	}
	return AnswerMarkerPrefix + string(data)
}

// ReturnPathLineEligible reports whether a recorded answer carries the
// per-question session line: the question opted in (return_path true) and
// names the session the line goes to, and both config switches agree —
// autonomous.questions.enabled (the whole question channel) and
// autonomous.questions.return_path (this return path specifically). Both
// are read live at answer time; absent keys count as on.
func ReturnPathLineEligible(question Question, cfgEnabled, cfgReturnPath bool) bool {
	return cfgEnabled && cfgReturnPath && question.ReturnPath && question.SessionID != ""
}

// QuestionAnswerReturnPath delivers the per-question session line. The
// daemon wires it once at start (cmd/datawatch/main.go); the Sender is the
// small seam the tests fake.
type QuestionAnswerReturnPath struct {
	// Sender delivers one built line to a live session by its id — the
	// same send the session router uses for operator input. A non-nil
	// error means the session is gone or cannot accept input. nil = no
	// session line (the default return path — event plus polled GET —
	// still carries the answer).
	Sender func(sessionID, line string) error

	// Log receives the one warning line when a delivery fails; nil = no
	// warning.
	Log func(format string, args ...any)
}

// NewQuestionAnswerReturnPath binds a sender and a warning logger into a
// deliverer. Either argument may be nil.
func NewQuestionAnswerReturnPath(sender func(string, string) error, logf func(string, ...any)) *QuestionAnswerReturnPath {
	return &QuestionAnswerReturnPath{Sender: sender, Log: logf}
}

// Send delivers the session line for one recorded answer when the question
// opted in and the config allows it, from the goroutine it starts itself.
// It never blocks the caller and can never fail the answer call: a gone
// session or a send error is one warning line, and the answer stays in the
// store — the event and the polled GET still carry it.
func (d *QuestionAnswerReturnPath) Send(question Question, answer Answer, cfgEnabled, cfgReturnPath bool) {
	if d == nil {
		return
	}
	sender := d.Sender
	if sender == nil {
		return
	}
	if !ReturnPathLineEligible(question, cfgEnabled, cfgReturnPath) {
		return
	}
	sessionID := question.SessionID
	line := BuildAnswerMarkerLine(question, answer)
	qid, aid := question.ID, answer.ID
	logf := d.Log
	go func() {
		defer func() { _ = recover() }()
		if err := sender(sessionID, line); err != nil && logf != nil {
			logf("question return path: answer %s for question %s not delivered to session %s: %v — the answer stays in the store; the event and the polled GET still carry it",
				aid, qid, sessionID, err)
		}
	}()
}
