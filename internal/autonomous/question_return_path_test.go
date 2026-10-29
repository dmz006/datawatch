package autonomous

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fakes ---------------------------------------------------------------

// returnPathSenderFake is the test double for the session sender: it
// records every line and reports a configurable error (a "gone" session).
type returnPathSenderFake struct {
	mu    sync.Mutex
	lines []string
	err   error
	done  chan string
	calls int
}

func newReturnPathSenderFake(err error) *returnPathSenderFake {
	return &returnPathSenderFake{err: err, done: make(chan string, 4)}
}

func (f *returnPathSenderFake) send(sessionID, line string) error {
	f.mu.Lock()
	f.lines = append(f.lines, line)
	f.mu.Unlock()
	select {
	case f.done <- line:
	default:
	}
	return f.err
}

// linesSeen returns the lines delivered so far.
func (f *returnPathSenderFake) linesSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.lines))
	copy(out, f.lines)
	return out
}

// deliver returns the next delivered line, or fails the test if none
// arrives within a short grace window (long enough that "sent" is
// unambiguous, short enough that "not sent" is).
func (f *returnPathSenderFake) deliver(t *testing.T) string {
	t.Helper()
	select {
	case line := <-f.done:
		return line
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("expected a delivered session line, none arrived within the grace window")
		return ""
	}
}

// noLineWithin lets a test assert that nothing was sent.
func (f *returnPathSenderFake) noLineWithin(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-f.done:
		t.Fatalf("expected no delivered session line, but one arrived: %s", <-f.done)
	case <-time.After(d):
	}
}

func returnPathTestQuestion() Question {
	return Question{
		ID:         "q-1",
		Context:    QuestionContextNone,
		Default:    "first",
		Options:    []QuestionOption{{ID: "first", Label: "first"}, {ID: "second", Label: "second"}},
		ReturnPath: true,
		SessionID:  "sess-asking",
	}
}

func returnPathTestAnswer() Answer {
	return Answer{
		ID:             "a-1",
		QuestionID:     "q-1",
		Actor:          "operator-7",
		Surface:        "rest",
		ChosenOptionID: "first",
		Quote:          "go with the first option",
	}
}

func payloadOf(t *testing.T, line string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(line, AnswerMarkerPrefix) {
		t.Fatalf("line does not start with the stable prefix %q: %q", AnswerMarkerPrefix, line)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, AnswerMarkerPrefix)), &fields); err != nil {
		t.Fatalf("marker payload is not JSON: %v (line: %q)", err, line)
	}
	return fields
}

// --- tests -----------------------------------------------------------------

// TestQuestionReturnPathMarkerPrefix — the exact wire shape: the fixed
// marker "DATAWATCH_ANSWER:", one space, one line of JSON, and no overlap
// with the marker convention the answer path depends on.
func TestQuestionReturnPathMarkerPrefix(t *testing.T) {
	line := BuildAnswerMarkerLine(returnPathTestQuestion(), returnPathTestAnswer())

	if !strings.HasPrefix(line, AnswerMarkerPrefix) {
		t.Fatalf("line must start with %q; got %q", AnswerMarkerPrefix, line)
	}
	// Exactly one line: no raw newline, no carriage return — the only
	// CR/newline bytes in the whole line are inside JSON string escapes.
	if strings.IndexByte(line, '\n') >= 0 || strings.IndexByte(line, '\r') >= 0 {
		t.Fatalf("line must be exactly one line (no raw newline); got %q", line)
	}
	// The fixed marker plus one space: the byte after the colon is a space,
	// and the next byte starts the JSON object.
	if !strings.HasPrefix(line, "DATAWATCH_ANSWER: {") {
		t.Fatalf("line must be the marker, one space, then JSON; got %q", line)
	}
	// Not misread as, and not a prefix of, the other session markers.
	if strings.HasPrefix(line, "DATAWATCH_COMPLETE:") || strings.HasPrefix(line, "DATAWATCH_NEEDS_INPUT:") {
		t.Fatalf("line must not start with another session marker: %q", line)
	}
	if strings.HasPrefix("DATAWATCH_COMPLETE:", AnswerMarker) || strings.HasPrefix("DATAWATCH_NEEDS_INPUT:", AnswerMarker) {
		t.Fatal("marker must be distinct from the complete and needs-input markers")
	}
	// The JSON payload is compact one-line (no raw whitespace before the
	// first object byte).
	if !strings.HasPrefix(line, AnswerMarkerPrefix+`{"`) {
		t.Fatalf("payload must be compact JSON immediately after the marker; got %q", line)
	}
}

// TestQuestionReturnPathJSONFields — the payload parses and carries all
// the fields: question_id, answer_id, chosen_option, quote (verbatim),
// actor, at (RFC 3339 UTC).
func TestQuestionReturnPathJSONFields(t *testing.T) {
	q := returnPathTestQuestion()
	a := returnPathTestAnswer()
	created := time.Date(2026, 11, 25, 17, 4, 9, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	a.CreatedAt = &created

	line := BuildAnswerMarkerLine(q, a)
	fields := payloadOf(t, line)

	// Exactly the six documented keys — no more, no fewer.
	wantKeys := map[string]bool{
		"question_id":   true,
		"answer_id":     true,
		"chosen_option": true,
		"quote":         true,
		"actor":         true,
		"at":            true,
	}
	if len(fields) != len(wantKeys) {
		t.Fatalf("payload must carry exactly 6 keys; got %d: %v", len(fields), fields)
	}
	for k, v := range fields {
		if !wantKeys[k] {
			t.Fatalf("unexpected payload key %q", k)
		}
		if s, ok := v.(string); !ok || s == "" {
			t.Fatalf("key %q must be a non-empty string; got %v", k, v)
		}
	}
	if fields["question_id"] != q.ID {
		t.Fatalf("question_id = %v; want %q", fields["question_id"], q.ID)
	}
	if fields["answer_id"] != a.ID {
		t.Fatalf("answer_id = %v; want %q", fields["answer_id"], a.ID)
	}
	if fields["chosen_option"] != a.ChosenOptionID {
		t.Fatalf("chosen_option = %v; want %q", fields["chosen_option"], a.ChosenOptionID)
	}
	if fields["quote"] != a.Quote {
		t.Fatalf("quote not verbatim: got %v; want %q", fields["quote"], a.Quote)
	}
	if fields["actor"] != a.Actor {
		t.Fatalf("actor = %v; want %q", fields["actor"], a.Actor)
	}
	// at: RFC 3339, and UTC even though the record was kept in local —
	// on the wire that reads as the "Z" suffix.
	atStr, _ := fields["at"].(string)
	at, err := time.Parse(time.RFC3339, atStr)
	if err != nil {
		t.Fatalf("at is not RFC 3339: %q (%v)", atStr, err)
	}
	if !strings.HasSuffix(atStr, "Z") {
		t.Fatalf("at must be UTC (\"Z\" suffix); got %q", atStr)
	}
	if want := created.UTC(); !at.Equal(want) {
		t.Fatalf("at = %q; want %s", atStr, want.Format(time.RFC3339))
	}
}

// TestQuestionReturnPathMultilineQuoteStaysOneLine — verbatim quotes with
// newlines, tabs, double quotes and escapes still ship as one line, and
// the quote round-trips exactly.
func TestQuestionReturnPathMultilineQuoteStaysOneLine(t *testing.T) {
	q := returnPathTestQuestion()
	a := returnPathTestAnswer()
	a.Quote = "first line\nsecond\tline (with a tab)\n\"quoted: DATAWATCH_NEEDS_INPUT: x\" and backslash \\ and unicode ✓"

	line := BuildAnswerMarkerLine(q, a)

	if strings.IndexByte(line, '\n') >= 0 || strings.IndexByte(line, '\r') >= 0 {
		t.Fatalf("line must stay one line with a multi-line quote; got %q", line)
	}
	fields := payloadOf(t, line)
	if got, ok := fields["quote"].(string); !ok || got != a.Quote {
		t.Fatalf("quote did not round-trip verbatim: got %v; want %q", fields["quote"], a.Quote)
	}
}

// TestQuestionReturnPathGoneSessionLeavesAnswerQueued — a delivery failure
// (session gone) is one warning line and nothing else: the answer stays
// recorded in the store (the event and the polled GET still carry it), and
// the answer call itself was never at risk.
func TestQuestionReturnPathGoneSessionLeavesAnswerQueued(t *testing.T) {
	store, err := NewQuestionStore(t.TempDir() + "/questions.jsonl")
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	qIn := returnPathTestQuestion()
	qIn.Text = "which deployment order"
	q, err := store.Raise(qIn)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	a, err := store.Answer(q.ID, Answer{
		Actor:          "operator-7",
		Surface:        "comm:teams",
		ChosenOptionID: "second",
		Quote:          "second, with a rollout gate",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	type warn struct {
		mu    sync.Mutex
		count int
		lines []string
	}
	w := &warn{}
	d := NewQuestionAnswerReturnPath(
		func(sessionID, line string) error {
			return errors.New(`session sess-asking: not found (gone)`)
		},
		func(format string, args ...any) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.count++
			w.lines = append(w.lines, format)
		},
	)
	d.Send(q, a, true, true) // the answer call itself returned long since

	// The answer is still recorded: queued view, count and current all agree.
	if cur, ok := store.CurrentAnswer(q.ID); !ok || cur.ID != a.ID || cur.Quote != a.Quote {
		t.Fatalf("answer must stay recorded in the store after a failed delivery; got ok=%v cur=%+v", ok, cur)
	}
	if n := store.AnswerCount(q.ID); n != 1 {
		t.Fatalf("AnswerCount = %d; want 1 (the answer stays queued)", n)
	}
	if views, ok := store.Answers(q.ID); !ok || len(views) != 1 {
		t.Fatalf("Answers = %d views (ok=%v); want 1", len(views), ok)
	}
	if st, ok := store.Get(q.ID); !ok || st.State != QuestionAnswered {
		t.Fatalf("question state after failed delivery = %q (ok=%v); want answered", st.State, ok)
	}

	// And exactly one warning line, naming the failure — never zero, never
	// more.
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		n := w.count
		w.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w.count != 1 {
		t.Fatalf("want exactly one warning line for the failed delivery; got %d", w.count)
	}
}

// TestQuestionReturnPathOffSendsNothing — return_path false on the
// question, the autonomous.questions.return_path toggle off, the whole
// switch off, or no session id: none of them delivers a line.
func TestQuestionReturnPathOffSendsNothing(t *testing.T) {
	fake := newReturnPathSenderFake(nil)
	d := NewQuestionAnswerReturnPath(fake.send, nil)
	a := returnPathTestAnswer()

	qOptIn := returnPathTestQuestion() // return_path true + session id
	d.Send(qOptIn, a, true, false)     // autonomous.questions.return_path toggle off
	d.Send(qOptIn, a, false, true)     // autonomous.questions switch off

	qOptOut := returnPathTestQuestion()
	qOptOut.ReturnPath = false
	d.Send(qOptOut, a, true, true)

	qNoSession := returnPathTestQuestion()
	qNoSession.SessionID = ""
	d.Send(qNoSession, a, true, true)

	fake.noLineWithin(t, 300*time.Millisecond)
	if got := fake.linesSeen(); len(got) != 0 {
		t.Fatalf("expected no delivered lines; got %d: %v", len(got), got)
	}
}

// TestQuestionReturnPathSupersedingAnswerSendsNewLine — a changed mind is a
// new answer (superseding), and it ships its own new line: same marker,
// the new answer_id and chosen_option, the old line untouched.
func TestQuestionReturnPathSupersedingAnswerSendsNewLine(t *testing.T) {
	fake := newReturnPathSenderFake(nil)
	d := NewQuestionAnswerReturnPath(fake.send, nil)
	q := returnPathTestQuestion()

	a1 := returnPathTestAnswer()
	a2 := Answer{
		ID:             "a-2",
		QuestionID:     q.ID,
		Actor:          "operator-7",
		Surface:        "rest",
		ChosenOptionID: "second",
		Quote:          "actually second — mind changed",
		Supersedes:     a1.ID,
	}

	d.Send(q, a1, true, true)
	line1 := fake.deliver(t)
	d.Send(q, a2, true, true)
	line2 := fake.deliver(t)

	if !strings.HasPrefix(line1, AnswerMarkerPrefix) || !strings.HasPrefix(line2, AnswerMarkerPrefix) {
		t.Fatalf("both lines must carry the fixed marker prefix; got %q and %q", line1, line2)
	}
	if line1 == line2 {
		t.Fatalf("a superseding answer must ship a new line, not a repeat of the first: %q", line1)
	}
	f1 := payloadOf(t, line1)
	f2 := payloadOf(t, line2)
	if f1["answer_id"] != "a-1" || f1["chosen_option"] != "first" {
		t.Fatalf("first line fields wrong: answer_id=%v chosen_option=%v", f1["answer_id"], f1["chosen_option"])
	}
	if f2["answer_id"] != "a-2" || f2["chosen_option"] != "second" {
		t.Fatalf("superseding line fields wrong: answer_id=%v chosen_option=%v", f2["answer_id"], f2["chosen_option"])
	}
	if f2["quote"] != a2.Quote {
		t.Fatalf("superseding line quote not verbatim: %v", f2["quote"])
	}
	if want := []string{line1, line2}; len(fake.linesSeen()) != 2 || fake.linesSeen()[0] != want[0] || fake.linesSeen()[1] != want[1] {
		t.Fatalf("expected exactly the two lines in order; got %v", fake.linesSeen())
	}
}
