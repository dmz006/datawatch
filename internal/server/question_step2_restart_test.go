package server

// PRD step 2 — restart + happy path.
//
// A question that was raised and answered before a daemon restart must
// produce EXACTLY ONE question.answered lifecycle event in total: the
// live one at answer time. Reopening the same store file and running the
// daemon's startup reconcile (the daemon's own order: emit each pending
// event, then mark it) must not re-emit it — once — and it must not
// start duplicating it on further restarts. Separately, the full happy
// path driven over HTTP: a raise with return_path plus high priority
// fires the WS broadcast, the router send and the alert; the answer
// fires the queued event and the DATAWATCH_ANSWER: marker line; the
// polled GET read shows the answer; and the opted-in asking session
// gets exactly one marker line on its input.

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestQuestionStep2Restarts_AnsweredEventExactlyOnce: raise → answer
// (one live event, one marker line) → close the store and reopen the
// same file (daemon restart) → reconcile → still exactly one event in
// total; the daemon's second restart reconciles to zero re-emissions and
// the count stays exactly one.
func TestQuestionStep2Restarts_AnsweredEventExactlyOnce(t *testing.T) {
	f := qstep2New(t, true)

	// Live lifecycle: raise, then answer.
	id := qstep2RaiseID(t, requireRaise(t, f))
	rr := doQ(t, f.s, http.MethodPost, "/api/questions/"+id+"/answer", qstep2Answer())
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer → %d (%s), want 201", rr.Code, rr.Body.String())
	}
	evs := f.queue.waitFor(t, 1, "question.answered")
	if len(evs) != 1 || evs[0].Kind != autonomous.KindQuestionAnswered {
		t.Fatalf("events after answer = %v, want exactly one %q", evs, autonomous.KindQuestionAnswered)
	}
	waitTrue(t, 2*time.Second, func() bool { return markerLineCount(t, f.path) == 1 })
	if got := markerLineCount(t, f.path); got != 1 {
		t.Fatalf("marker lines after answer = %d, want 1", got)
	}

	// qstep2Restart simulates one daemon restart on the same temp dir:
	// a fresh manager (fresh pending queue), a reopened store over the
	// same file, the daemon's sink wiring, and main.go's startup
	// reconcile — emit each held event, then mark it.
	qstep2Restart := func() *questionEventCapture {
		mgr := new(autonomous.Manager)
		capture := &questionEventCapture{}
		cancel := mgr.Subscribe(autonomous.LifecycleFilter{
			Kinds: []string{autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn},
		}, capture.push)
		t.Cleanup(cancel)
		st, err := autonomous.NewQuestionStore(f.path)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		st.SetEventSink(mgr.EmitQuestionEvent)
		for _, ev := range st.ReconcileEmittedEvents() {
			mgr.EmitQuestionEvent(ev)
			if !st.MarkEventEmitted(ev) {
				t.Fatalf("mark reconciled event %s: the daemon's own log says it will re-fire next start", ev.Kind)
			}
		}
		return capture
	}

	// Restart #1: the marker was written in the live window before the
	// crash, so reconcile holds nothing and the new queue stays empty —
	// the event the operator saw at answer time is the only one.
	cap1 := qstep2Restart()
	if got := len(cap1.all()); got != 0 {
		t.Fatalf("restart #1 re-emitted %d events, want 0 (the marker is already written)", got)
	}

	// Restart #2: same file, same marker — reconcile must stay silent.
	cap2 := qstep2Restart()
	if got := len(cap2.all()); got != 0 {
		t.Fatalf("restart #2 re-emitted %d events, want 0 (no duplicates after further restarts)", got)
	}

	// Exactly one question.answered event in total across every queue
	// the daemon ever ran, and exactly one marker line on disk.
	total := 0
	for _, c := range []*questionEventCapture{f.queue, cap1, cap2} {
		for _, ev := range c.all() {
			if ev.Kind == autonomous.KindQuestionAnswered {
				total++
			}
		}
	}
	if total != 1 {
		t.Fatalf("question.answered events across live window + restarts = %d, want exactly 1", total)
	}
	if got := markerLineCount(t, f.path); got != 1 {
		t.Fatalf("marker lines after restarts = %d, want 1", got)
	}

	// The store's own bookkeeping survived the restarts too.
	if st, err := autonomous.NewQuestionStore(f.path); err != nil {
		t.Fatalf("final reopen: %v", err)
	} else if q, ok := st.Get(id); !ok || q.State != autonomous.QuestionAnswered {
		t.Fatalf("question after restarts = %+v, want answered", q)
	}
}

// qstep2Inbox is the fake session input: it records every line the
// question answer return path delivers to the asking session, exactly
// once each, like a real session prompt would consume them.
type qstep2Inbox struct {
	mu    sync.Mutex
	lines []string
}

// Send is the deliverer's sender seam — the fake of the daemon's
// mgr.SendInput. It records and never fails (a failed send is a log
// line, never an error, in the real wiring too).
func (in *qstep2Inbox) Send(sessionID, line string) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.lines = append(in.lines, line)
	return nil
}

func (in *qstep2Inbox) All() []string {
	in.mu.Lock()
	defer in.mu.Unlock()
	out := make([]string, len(in.lines))
	copy(out, in.lines)
	return out
}

// TestQuestionStep2HappyPath_FullFlow: raise with return_path + high
// priority → the WS broadcast, the router send and the alert all fire;
// the answer → the queued event, exactly one marker line, and exactly
// one DATAWATCH_ANSWER: line on the asking session's fake input; the
// polled GET read shows the answer.
func TestQuestionStep2HappyPath_FullFlow(t *testing.T) {
	f := qstep2New(t, true)

	inbox := &qstep2Inbox{}
	f.s.SetQuestionAnswerReturnPath(autonomous.NewQuestionAnswerReturnPath(
		inbox.Send,
		func(format string, args ...any) {},
	))

	// Raise with the return path opt-in and the high priority.
	body := qstep2Body()
	body["return_path"] = true
	rr := doQ(t, f.s, http.MethodPost, "/api/questions", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("raise → %d (%s), want 201", rr.Code, rr.Body.String())
	}
	var raised struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &raised); err != nil {
		t.Fatalf("decode raise: %v", err)
	}
	id := raised.Question.ID

	// The three notify legs, all of them.
	waitTrue(t, 2*time.Second, func() bool { return f.notifier.CallCount() == 1 })
	if got := f.notifier.CallCount(); got != 1 {
		t.Fatalf("router sends = %d, want 1", got)
	}
	frames := drainHubFrames(t, f.hub, 500*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("%q WS frames = %d, want 1", MsgQuestionRaised, got)
	}
	if got := countFrames(frames, MsgAlert); got != 1 {
		t.Fatalf("%q WS frames = %d, want 1", MsgAlert, got)
	}
	if got := len(f.ast.List()); got != 1 {
		t.Fatalf("alerts = %d, want 1", got)
	}

	// Answer: the queued event, the marker line…
	rr = doQ(t, f.s, http.MethodPost, "/api/questions/"+id+"/answer", qstep2Answer())
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer → %d (%s), want 201", rr.Code, rr.Body.String())
	}
	if evs := f.queue.waitFor(t, 1, "question.answered"); len(evs) != 1 {
		t.Fatalf("events after answer = %v, want exactly one", evs)
	}
	waitTrue(t, 2*time.Second, func() bool { return markerLineCount(t, f.path) == 1 })
	if got := markerLineCount(t, f.path); got != 1 {
		t.Fatalf("marker lines = %d, want 1", got)
	}

	// …and exactly one DATAWATCH_ANSWER: line on the fake session input.
	waitTrue(t, 2*time.Second, func() bool { return len(inbox.All()) == 1 })
	if got := len(inbox.All()); got != 1 {
		t.Fatalf("session inbox = %v, want exactly one line", inbox.All())
	}
	lines := inbox.All()
	line := lines[0]
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("return path delivered %q — must be exactly one line", line)
	}
	if !strings.HasPrefix(line, autonomous.AnswerMarkerPrefix) {
		t.Fatalf("return path line %q lacks the %q prefix", line, autonomous.AnswerMarkerPrefix)
	}
	var payload struct {
		QuestionID   string `json:"question_id"`
		ChosenOption string `json:"chosen_option"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, autonomous.AnswerMarkerPrefix)), &payload); err != nil {
		t.Fatalf("decode marker payload: %v (%s)", err, line)
	}
	if payload.QuestionID != id {
		t.Fatalf("marker payload question_id = %q, want %q", payload.QuestionID, id)
	}
	if payload.ChosenOption != "yes" {
		t.Fatalf("marker payload chosen_option = %q, want yes", payload.ChosenOption)
	}

	// The polled GET read shows the answer: the question is answered
	// and the current answer's option is on the same read.
	rr = doQ(t, f.s, http.MethodGet, "/api/questions/"+id, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/questions/%s → %d (%s), want 200", id, rr.Code, rr.Body.String())
	}
	var got struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Question.State != autonomous.QuestionAnswered {
		t.Fatalf("GET state = %q, want answered", got.Question.State)
	}
	if q, ok := f.store.Answers(id); !ok || len(q) != 1 || q[0].ChosenOptionID != "yes" {
		t.Fatalf("answer log = %v, want exactly one answer choosing yes", q)
	}
}
