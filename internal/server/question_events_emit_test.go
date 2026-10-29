// question_events_emit_test.go
//
// The question lifecycle events (question.answered / question.withdrawn)
// flow through the same dispatcher and per-subscriber queue as every other
// kind: the store builds and fires the event on each answer and each
// withdrawal, then appends its question_event_emitted marker line so the
// event stays exactly once across reopens and across the crash window
// between the answer write and the enqueue. These tests drive the real
// question store, the real manager dispatcher and the real REST handlers;
// a recording subscriber stands in for the daemon's own sinks.
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// -- recording subscriber -------------------------------------------------

// questionEventCapture records the events one subscriber receives from the
// manager's dispatcher.
type questionEventCapture struct {
	mu     sync.Mutex
	events []autonomous.LifecycleEvent
}

func (c *questionEventCapture) push(ev autonomous.LifecycleEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *questionEventCapture) all() []autonomous.LifecycleEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]autonomous.LifecycleEvent, len(c.events))
	copy(out, c.events)
	return out
}

// waitFor polls until the capture holds want events (delivery happens on the
// subscriber's own goroutine, never in the emit call).
func (c *questionEventCapture) waitFor(t *testing.T, want int, what string) []autonomous.LifecycleEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		all := c.all()
		if len(all) >= want {
			return all
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (captured %d events: %v)", what, len(c.all()), c.all())
	return nil
}

// -- fixture ----------------------------------------------------------------

// qeNew wires the daemon's real emit path — the store's event sink pointed
// at (a zero-value) Manager.EmitQuestionEvent, the same dispatcher the
// daemon's subscription sinks hang off — and subscribes the recording
// capture to the two question kinds. The store path is returned so tests
// can reopen the same file.
func qeNew(t *testing.T) (store *autonomous.QuestionStore, path string, cap *questionEventCapture) {
	t.Helper()
	mgr := new(autonomous.Manager) // zero value: the dispatcher is lazy
	cap = &questionEventCapture{}
	cancel := mgr.Subscribe(autonomous.LifecycleFilter{
		Kinds: []string{autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn},
	}, cap.push)
	t.Cleanup(cancel)

	path = filepath.Join(t.TempDir(), "questions.jsonl")
	st, err := autonomous.NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	st.SetEventSink(mgr.EmitQuestionEvent) // the daemon's wiring
	return st, path, cap
}

// qeRaise raises one open question on the store. The record — not the
// event — is what the tests assert the store holds.
func qeRaise(t *testing.T, store *autonomous.QuestionStore) autonomous.Question {
	t.Helper()
	q, err := store.Raise(autonomous.Question{
		Label:     "decision",
		AskerKind: autonomous.QuestionAskerSystem,
		Context:   autonomous.QuestionContextNone,
		Risk:      autonomous.QuestionRiskLow,
		Text:      "proceed with the migration?",
		Options: []autonomous.QuestionOption{
			{ID: "go", Label: "go"},
			{ID: "stop", Label: "stop"},
		},
		Default: "go",
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	return q
}

// markerLineCount counts the question_event_emitted marker lines in the
// store file.
func markerLineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"type":"question_event_emitted"`) {
			n++
		}
	}
	return n
}

// -- tests -------------------------------------------------------------------

// TestQuestionEventEmitAnswer: answering an open question enqueues exactly
// one question.answered event into the dispatcher the daemon's sinks hang
// off; the subscriber receives it with the question, the answer and a
// single-line description; the store appends its marker.
func TestQuestionEventEmitAnswer(t *testing.T) {
	store, path, cap := qeNew(t)
	q := qeRaise(t, store)

	a, err := store.Answer(q.ID, autonomous.Answer{
		Actor:          "operator",
		Surface:        "test",
		ChosenOptionID: "go",
		Quote:          "ship it, the rollout plan covers rollback",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	events := cap.waitFor(t, 1, "the question.answered event")
	if len(events) != 1 {
		t.Fatalf("captured %d events, want exactly 1", len(events))
	}
	ev := events[0]
	if ev.Kind != autonomous.KindQuestionAnswered {
		t.Errorf("event kind = %q, want %q", ev.Kind, autonomous.KindQuestionAnswered)
	}
	if ev.QuestionID != q.ID {
		t.Errorf("event question_id = %q, want %q", ev.QuestionID, q.ID)
	}
	if ev.AnswerID != a.ID {
		t.Errorf("event answer_id = %q, want %q", ev.AnswerID, a.ID)
	}
	if ev.Details == "" || strings.ContainsAny(ev.Details, "\r\n") {
		t.Errorf("event details %q: want a non-empty single line", ev.Details)
	}
	if !ev.At.IsZero() && ev.ID == "" {
		t.Error("event has no id (emit must stamp one)")
	}

	if n := markerLineCount(t, path); n != 1 {
		t.Errorf("marker lines in store file = %d, want 1 (the event must be marked before the store reports it emitted)", n)
	}
	if got, ok := store.CurrentAnswer(q.ID); !ok || got.ID != a.ID {
		t.Errorf("current answer = %q (ok=%v), want %q", got.ID, ok, a.ID)
	}
}

// TestQuestionEventEmitWithdraw: withdrawing an open question enqueues
// exactly one question.withdrawn event — no answer involved, never one for
// an answer — and the subscriber receives it.
func TestQuestionEventEmitWithdraw(t *testing.T) {
	store, path, cap := qeNew(t)
	q := qeRaise(t, store)

	out, err := store.Withdraw(q.ID, "alice", "duplicated by another raise")
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if out.State != autonomous.QuestionWithdrawn {
		t.Fatalf("withdrawn question state = %q, want withdrawn", out.State)
	}

	events := cap.waitFor(t, 1, "the question.withdrawn event")
	if len(events) != 1 {
		t.Fatalf("captured %d events, want exactly 1", len(events))
	}
	ev := events[0]
	if ev.Kind != autonomous.KindQuestionWithdrawn {
		t.Errorf("event kind = %q, want %q", ev.Kind, autonomous.KindQuestionWithdrawn)
	}
	if ev.QuestionID != q.ID {
		t.Errorf("event question_id = %q, want %q", ev.QuestionID, q.ID)
	}
	if ev.AnswerID != "" {
		t.Errorf("withdrawal event carries answer_id %q, want empty", ev.AnswerID)
	}
	if ev.Details == "" || strings.ContainsAny(ev.Details, "\r\n") {
		t.Errorf("event details %q: want a non-empty single line", ev.Details)
	}
	if n := markerLineCount(t, path); n != 1 {
		t.Errorf("marker lines in store file = %d, want 1", n)
	}
}

// TestQuestionEventEmitSupersedingAnswer: a changed mind is a new answer
// that supersedes the old one, and it fires its own event: two answers, two
// events, each naming its own answer record. The first event stays — the
// record is append-only, and the events already delivered are not recalled.
func TestQuestionEventEmitSupersedingAnswer(t *testing.T) {
	store, path, cap := qeNew(t)
	q := qeRaise(t, store)

	first, err := store.Answer(q.ID, autonomous.Answer{
		Actor: "operator", Surface: "test", ChosenOptionID: "go",
	})
	if err != nil {
		t.Fatalf("first Answer: %v", err)
	}
	second, err := store.Answer(q.ID, autonomous.Answer{
		Actor: "operator", Surface: "test", ChosenOptionID: "stop",
		Quote: "wait for the freeze window", Supersedes: first.ID,
	})
	if err != nil {
		t.Fatalf("superseding Answer: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("a superseding answer must carry its own id")
	}

	events := cap.waitFor(t, 2, "both question.answered events")
	if len(events) != 2 {
		t.Fatalf("captured %d events, want 2 (one per answer)", len(events))
	}
	seen := map[string]string{} // answer id in the event -> kind
	for _, ev := range events {
		if ev.Kind != autonomous.KindQuestionAnswered || ev.QuestionID != q.ID {
			t.Errorf("event %q: kind/question = %s/%q, want %q/%q", ev.ID, ev.Kind, ev.QuestionID, autonomous.KindQuestionAnswered, q.ID)
		}
		seen[ev.AnswerID] = ev.Kind
	}
	if _, ok := seen[first.ID]; !ok {
		t.Errorf("the first answer %s has no event (got answers: %v)", first.ID, keys(seen))
	}
	if _, ok := seen[second.ID]; !ok {
		t.Errorf("the superseding answer %s has no event (got answers: %v)", second.ID, keys(seen))
	}

	// Two answers, two markers: the reopen view re-fires nothing.
	if n := markerLineCount(t, path); n != 2 {
		t.Errorf("marker lines in store file = %d, want 2 (one per answer)", n)
	}
	if n := store.AnswerCount(q.ID); n != 2 {
		t.Errorf("answer count = %d, want 2", n)
	}
}

// keys is the map-key list for an error message.
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestQuestionEventEmitMarkerPreventsDuplicatesAfterReopen: the marker line
// is what proves the event went into the queue. Reopen the same file with a
// fresh store, a fresh manager and a fresh subscriber: the answered
// question is already marked, so the startup reconcile lists nothing and no
// event is fired twice.
func TestQuestionEventEmitMarkerPreventsDuplicatesAfterReopen(t *testing.T) {
	store, path, cap := qeNew(t)
	q := qeRaise(t, store)
	a, err := store.Answer(q.ID, autonomous.Answer{
		Actor: "operator", Surface: "test", ChosenOptionID: "go",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	cap.waitFor(t, 1, "the question.answered event")
	if n := markerLineCount(t, path); n != 1 {
		t.Fatalf("marker lines before reopen = %d, want 1", n)
	}

	// The daemon restarts: fresh view over the identical file, fresh emit
	// path, another subscriber — exactly the daemon's wiring at startup.
	reopened, err := autonomous.NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen NewQuestionStore: %v", err)
	}
	if cur, ok := reopened.CurrentAnswer(q.ID); !ok || cur.ID != a.ID {
		t.Fatalf("reopened view lost the answer (got %q, ok=%v), want %q", cur.ID, ok, a.ID)
	}
	if got := reopened.List(autonomous.QuestionFilter{}); len(got) != 1 || got[0].State != autonomous.QuestionAnswered {
		t.Fatalf("reopened list = %d questions (states %v), want 1 answered", len(got), states(got))
	}

	reopenedM := new(autonomous.Manager)
	reopenedCap := &questionEventCapture{}
	cancel := reopenedM.Subscribe(autonomous.LifecycleFilter{Kinds: []string{
		autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn,
	}}, reopenedCap.push)
	defer cancel()
	reopened.SetEventSink(reopenedM.EmitQuestionEvent)

	// The daemon's startup reconcile: fire what replay found unmarked.
	if pending := reopened.ReconcileEmittedEvents(); len(pending) != 0 {
		t.Fatalf("reconcile after a marked answer listed %d event(s), want 0 (the marker must prevent a duplicate)", len(pending))
	}
	time.Sleep(30 * time.Millisecond) // give any (wrong) delivery time to land
	if all := reopenedCap.all(); len(all) != 0 {
		t.Fatalf("reopened subscriber saw %d event(s) after a marked answer, want 0", len(all))
	}
	if n := markerLineCount(t, path); n != 1 {
		t.Errorf("marker lines after reopen = %d, want still 1 (no duplicate append)", n)
	}
}

// states is the state list for an error message.
func states(qs []autonomous.Question) []autonomous.QuestionState {
	out := make([]autonomous.QuestionState, len(qs))
	for i, q := range qs {
		out[i] = q.State
	}
	return out
}

// TestQuestionEventEmitCrashRepairedOnce: the crash window is a process
// death between the answer append and the enqueue — the file holds the
// answer but no marker. The daemon start's reconcile must find exactly that
// answer, fire once, mark it, and then stay quiet: the event is repaired
// exactly once, never twice.
func TestQuestionEventEmitCrashRepairedOnce(t *testing.T) {
	store, path, cap := qeNew(t)
	q := qeRaise(t, store)
	a, err := store.Answer(q.ID, autonomous.Answer{
		Actor: "operator", Surface: "test", ChosenOptionID: "go",
	})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	cap.waitFor(t, 1, "the live question.answered event")

	// Simulate the crash: the file keeps every line written — the question,
	// the answer — but loses the marker line, which never landed because
	// the process died before it was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	crashed := filepath.Join(filepath.Dir(path), "..", "crash", "questions.jsonl")
	crashed = filepath.Clean(crashed)
	if err := os.MkdirAll(filepath.Dir(crashed), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.Contains(line, `"type":"question_event_emitted"`) {
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(crashed, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write crashed file: %v", err)
	}

	// The daemon starts against the crashed file.
	store2, err := autonomous.NewQuestionStore(crashed)
	if err != nil {
		t.Fatalf("reopen crashed store: %v", err)
	}
	if cur, ok := store2.CurrentAnswer(q.ID); !ok || cur.ID != a.ID {
		t.Fatalf("crashed store lost the answer record (got %q, ok=%v)", cur.ID, ok)
	}
	mgr2 := new(autonomous.Manager)
	cap2 := &questionEventCapture{}
	cancel := mgr2.Subscribe(autonomous.LifecycleFilter{Kinds: []string{
		autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn,
	}}, cap2.push)
	defer cancel()
	store2.SetEventSink(mgr2.EmitQuestionEvent)

	// First reconcile: exactly the unmarked answered event, one entry.
	pending := store2.ReconcileEmittedEvents()
	if len(pending) != 1 {
		t.Fatalf("first reconcile listed %d event(s), want 1 (the crashed-window answer)", len(pending))
	}
	ev := pending[0]
	if ev.Kind != autonomous.KindQuestionAnswered || ev.QuestionID != q.ID || ev.AnswerID != a.ID {
		t.Fatalf("reconciled event = kind %q question %q answer %q, want %q/%q/%q",
			ev.Kind, ev.QuestionID, ev.AnswerID, autonomous.KindQuestionAnswered, q.ID, a.ID)
	}

	// The daemon fires it and then marks it, in the daemon's order.
	mgr2.EmitQuestionEvent(ev)
	if !store2.MarkEventEmitted(ev) {
		t.Fatal("MarkEventEmitted failed for the reconciled event")
	}

	// A subscriber sees exactly the one repaired event.
	events := cap2.waitFor(t, 1, "the repaired event")
	if len(events) != 1 {
		t.Fatalf("restarted subscriber captured %d event(s), want 1", len(events))
	}

	// Second reconcile (a later restart sees the marker): nothing.
	if pending := store2.ReconcileEmittedEvents(); len(pending) != 0 {
		t.Fatalf("second reconcile listed %d event(s), want 0 (the event was already repaired and marked)", len(pending))
	}
	time.Sleep(30 * time.Millisecond)
	if all := cap2.all(); len(all) != 1 {
		t.Fatalf("restarted subscriber holds %d event(s) in the end, want exactly 1 (repaired once, never twice)", len(all))
	}
	again, err := autonomous.NewQuestionStore(crashed) // yet another restart
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	if pending := again.ReconcileEmittedEvents(); len(pending) != 0 {
		t.Fatalf("reconcile after the marker was written listed %d event(s), want 0", len(pending))
	}
}

// TestQuestionEventEmitSwitchOff: with autonomous.questions.enabled off the
// daemon wires no sink and runs no reconcile (main.go), and the REST
// surface answers 503 before touching the store. Net: an answer attempted
// on that surface writes no record and fires no event, and even a record
// that does land through another path stays silent — the wiring condition
// is the switch, and off means no event, full stop.
func TestQuestionEventEmitSwitchOff(t *testing.T) {
	dir := t.TempDir()

	// 1. The REST surface with the switch off: 503 before the store.
	store, err := autonomous.NewQuestionStore(filepath.Join(dir, "off.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	q, err := store.Raise(autonomous.Question{
		Label: "decision", AskerKind: autonomous.QuestionAskerSystem,
		Context: autonomous.QuestionContextNone, Risk: autonomous.QuestionRiskLow,
		Text: "proceed?", Options: []autonomous.QuestionOption{{ID: "go", Label: "go"}, {ID: "stop", Label: "stop"}},
		Default: "go",
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	cfg := questionRESTTestConfig(false)
	s := &Server{cfg: &cfg, questionStore: store}

	// A subscriber is present so any mistaken delivery is caught.
	mgr := new(autonomous.Manager)
	cap := &questionEventCapture{}
	cancel := mgr.Subscribe(autonomous.LifecycleFilter{Kinds: []string{
		autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn,
	}}, cap.push)
	defer cancel()
	// The daemon's wiring condition for the switch off: the sink is not
	// set, and the daemon's reconcile loop over ReconcileEmittedEvents is
	// not run. The store below is left with its nil sink; it is routed to
	// no manager.

	rr := doQ(t, s, http.MethodPost, "/api/questions/"+q.ID+"/answer", map[string]any{
		"chosen_option_id": "go", "actor": "operator",
	})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("switch off: POST answer = %d (%s), want 503 naming the switch", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "autonomous.questions.enabled") {
		t.Errorf("503 should name the switch; got %q", rr.Body.String())
	}
	if n := store.AnswerCount(q.ID); n != 0 {
		t.Fatalf("switch off: store holds %d answer(s), want 0 (the 503 must precede the record)", n)
	}
	if st, _ := store.Get(q.ID); st.State != autonomous.QuestionOpen {
		t.Fatalf("switch off: question state = %q, want still open", st.State)
	}

	// 2. Wiring condition: even a record that lands through another path
	// stays silent without a sink — nothing fires, no marker is written,
	// and the reconcile list is only acted on by the daemon's switch-gated
	// loop (off means: not run).
	directPath := filepath.Join(dir, "off2", "direct.jsonl")
	if err := os.MkdirAll(filepath.Dir(directPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	direct, err := autonomous.NewQuestionStore(directPath)
	if err != nil {
		t.Fatalf("NewQuestionStore (direct): %v", err)
	}
	dq2, err := direct.Raise(autonomous.Question{
		Label: "decision", AskerKind: autonomous.QuestionAskerSystem,
		Context: autonomous.QuestionContextNone, Risk: autonomous.QuestionRiskLow,
		Text: "proceed?", Options: []autonomous.QuestionOption{{ID: "go", Label: "go"}, {ID: "stop", Label: "stop"}},
		Default: "go",
	})
	if err != nil {
		t.Fatalf("Raise (direct): %v", err)
	}
	if _, err := direct.Answer(dq2.ID, autonomous.Answer{
		Actor: "operator", Surface: "test", ChosenOptionID: "go",
	}); err != nil {
		t.Fatalf("direct Answer: %v", err)
	}
	// The daemon's wiring would route this record to a sink, and with the
	// switch on it would fire here. With the switch off there is no sink:
	// the record stands, nothing is emitted and no marker line lands.
	time.Sleep(30 * time.Millisecond) // give any (wrong) delivery time to land
	if all := cap.all(); len(all) != 0 {
		t.Fatalf("switch off: subscriber captured %d event(s), want none (the sink was not wired)", len(all))
	}
	if n := markerLineCount(t, directPath); n != 0 {
		t.Errorf("switch off: %d marker line(s) in the direct store, want none (no sink, no marker)", n)
	}
	// The unanswered list does change — it is a fact the daemon would
	// reconcile only when the switch is on and at start. It lists exactly
	// the unmarked answer.
	if pending := direct.ReconcileEmittedEvents(); len(pending) != 1 {
		t.Errorf("reconcile list = %d event(s), want 1 (the answer record is there; acting on it is the daemon's and its switch is off)", len(pending))
	}
}
