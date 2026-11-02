package server

// PRD step 2 — the autonomous.questions switch is the operator's off
// switch for the whole question channel: while it is off, a raise, an
// answer and a withdrawal produce NO WebSocket message, NO router send,
// NO alert, NO queued lifecycle event and NO marker line; flipping it on
// (live, no daemon restart — one Server, one store, one hub) makes them
// fire.
//
// The fixture drives the real HTTP handlers with fakes everywhere:
// the in-memory Hub (WS), a recording router (questionNotifyFake),
// an in-memory alert store, and a recording manager subscriber for the
// pending question queue. The only real thing is the append-only
// question store behind the handlers — the unit under test.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// qstep2Fix is the step-2 fixture: real handlers + real store, everything
// else a fake.
type qstep2Fix struct {
	s        *Server
	hub      *Hub
	ast      *alerts.Store
	notifier *questionNotifyFake
	queue    *questionEventCapture
	store    *autonomous.QuestionStore
	path     string
}

// qstep2New builds the fixture with the questions switch in `enabled`.
// The event sink follows the switch through the same two calls the
// daemon makes (ApplyEventSwitch at start, bound to the server so a
// config write re-applies it) — it is never attached unconditionally, so
// a live flip is the only thing that connects it.
func qstep2New(t *testing.T, enabled bool) *qstep2Fix {
	t.Helper()
	cfg := &config.Config{Autonomous: config.AutonomousConfig{
		Questions: config.AutonomousQuestionsConfig{Enabled: enabled},
	}}
	hub := NewHub()
	s := NewServer(hub, nil, "dw", "", nil, cfg, filepath.Join(t.TempDir(), "config.json"))
	ast, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatalf("alerts store: %v", err)
	}
	s.alertStore = ast

	path := filepath.Join(t.TempDir(), "questions.jsonl")
	st, err := autonomous.NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	mgr := new(autonomous.Manager)
	queue := &questionEventCapture{}
	cancel := mgr.Subscribe(autonomous.LifecycleFilter{
		Kinds: []string{autonomous.KindQuestionAnswered, autonomous.KindQuestionWithdrawn},
	}, queue.push)
	t.Cleanup(cancel)
	// t.Cleanup runs last-in-first-out: registered here, after the
	// t.TempDir() calls above, so it runs BEFORE those temp directories
	// are removed. A raise fan-out goroutine still in flight would
	// otherwise read/write the store and alerts files after they are
	// deleted — waiting for every notify goroutine first keeps the
	// cleanup order safe.
	t.Cleanup(s.WaitQuestionNotifies)
	applyEvents := func(on bool) { st.ApplyEventSwitch(on, mgr.EmitQuestionEvent, t.Logf) }
	applyEvents(enabled)
	s.SetQuestionEventsSwitch(applyEvents)

	nf := &questionNotifyFake{}
	s.SetQuestionRaiseNotifier(nf)
	s.SetQuestions(st)
	return &qstep2Fix{s: s, hub: hub, ast: ast, notifier: nf, queue: queue, store: st, path: path}
}

// qstep2Seed raises one open question directly on the store — how a
// question that was raised before the operator flipped the switch looks
// once it is off.
func qstep2Seed(t *testing.T, f *qstep2Fix) autonomous.Question {
	t.Helper()
	q, err := f.store.Raise(autonomous.Question{
		Label:   "decision",
		AskedBy: "qstep2-seed",
		Context: "none",
		Default: "yes",
		Options: []autonomous.QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}},
		Text:    "A question raised while the switch was on.",
	})
	if err != nil {
		t.Fatalf("seed raise: %v", err)
	}
	return q
}

// qstep2RaiseID decodes the raise response's question id.
func qstep2RaiseID(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode raise response: %v (%s)", err, rr.Body.String())
	}
	if resp.Question.ID == "" {
		t.Fatalf("raise response has no question id: %s", rr.Body.String())
	}
	return resp.Question.ID
}

// qstep2NoFanOut asserts the negative side of the off switch: none of
// the three notification legs, no lifecycle event, no marker line.
func qstep2NoFanOut(t *testing.T, f *qstep2Fix) {
	t.Helper()
	frames := drainHubFrames(t, f.hub, 300*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 0 {
		t.Fatalf("off switch produced %d %q WS frames, want 0", got, MsgQuestionRaised)
	}
	if got := countFrames(frames, MsgAlert); got != 0 {
		t.Fatalf("off switch produced %d %q WS frames, want 0", got, MsgAlert)
	}
	if got := f.notifier.CallCount(); got != 0 {
		t.Fatalf("off switch produced %d router sends, want 0", got)
	}
	if got := len(f.ast.List()); got != 0 {
		t.Fatalf("off switch produced %d alerts, want 0", got)
	}
	if got := len(f.queue.all()); got != 0 {
		t.Fatalf("off switch produced %d queued events, want 0", got)
	}
	if got := markerLineCount(t, f.path); got != 0 {
		t.Fatalf("off switch produced %d marker lines, want 0", got)
	}
}

// qstep2CountOf counts events of one kind in the fake pending queue.
func qstep2CountOf(t *testing.T, f *qstep2Fix, kind string) int {
	t.Helper()
	n := 0
	for _, ev := range f.queue.all() {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestQuestionStep2SwitchOff_FireNothing: while the switch is off, a
// raise, an answer and a withdrawal (even for a question that already
// exists in the store) are refused and fire none of the notification
// legs or event plumbing.
func TestQuestionStep2SwitchOff_FireNothing(t *testing.T) {
	f := qstep2New(t, false)
	id := qstep2Seed(t, f).ID

	// Raise — refused, and nothing fires.
	rr := doQ(t, f.s, http.MethodPost, "/api/questions", qstep2Body())
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/questions (off) → %d (%s), want 503", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "autonomous.questions.enabled") {
		t.Fatalf("503 body %q does not name the switch", rr.Body.String())
	}
	qstep2NoFanOut(t, f)

	// Answer — refused too (a seeded, live question is not exempt).
	rr = doQ(t, f.s, http.MethodPost, "/api/questions/"+id+"/answer", qstep2Answer())
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("answer (off) → %d (%s), want 503", rr.Code, rr.Body.String())
	}
	// Withdraw — ditto.
	rr = doQ(t, f.s, http.MethodPost, "/api/questions/"+id+"/withdraw", qstep2Withdraw())
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("withdraw (off) → %d (%s), want 503", rr.Code, rr.Body.String())
	}

	qstep2NoFanOut(t, f)
	if q, ok := f.store.Get(id); !ok || q.State != autonomous.QuestionOpen {
		t.Fatalf("question state after refused answer/withdraw = %+v, want open", q)
	}
}

// waitForCount polls get until it reads want and then moves on (the
// raise fan-out runs in its own goroutine, so an immediate read after the
// 201 can miss a leg that has not landed yet — the poll returns the
// moment the count holds). On timeout it fails the test, naming what it
// waited for, the wanted value and the last value seen, instead of
// falling through to the next assertion like waitTrue does.
func waitForCount(t *testing.T, what string, timeout time.Duration, get func() int, want int) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := get()
	for last != want {
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %s = %d; last saw %d", timeout, what, want, last)
		}
		time.Sleep(5 * time.Millisecond)
		last = get()
	}
	return last
}

// TestQuestionStep2SwitchOn_LiveFlip_FansOutAndEmits: the operator
// flips the switch on with the daemon's own path (PUT /api/config) — no
// restart, same Server, same store, same hub — and from that moment
// raise/answer/withdraw fire every notification leg and the lifecycle
// events plus one marker line per terminal op.
func TestQuestionStep2SwitchOn_LiveFlip_FansOutAndEmits(t *testing.T) {
	f := qstep2New(t, false)

	putJSON(t, f.s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.questions.enabled": true,
	})
	if !f.s.cfg.Autonomous.Questions.Enabled {
		t.Fatalf("live flip: switch still off after PUT /api/config")
	}

	// Raise (high priority, so the alert leg is in play) → 201 with all
	// three notify legs and no lifecycle event (raise is not one).
	// notifyQuestionRaised runs its legs sequentially in one goroutine —
	// hub, then router, then alert — so the alert leg is the last
	// side effect of a raise: waiting for it bars the whole fan-out, and
	// every read below happens after that barrier.
	id := qstep2RaiseID(t, requireRaise(t, f))
	waitForCount(t, "router sends after the first raise", 10*time.Second, f.notifier.CallCount, 1)
	if got := f.notifier.CallCount(); got != 1 {
		t.Fatalf("router sends after raise = %d, want 1", got)
	}
	// Wait for the last leg to land before draining frames, so the
	// alert's WS frame has been broadcast too.
	waitForCount(t, "the alert leg of the first raise (last fan-out leg)", 10*time.Second, func() int { return len(f.ast.List()) }, 1)
	frames := drainHubFrames(t, f.hub, 500*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("%q WS frames = %d, want 1", MsgQuestionRaised, got)
	}
	if got := countFrames(frames, MsgAlert); got != 1 {
		t.Fatalf("%q WS frames = %d, want 1", MsgAlert, got)
	}
	if got := len(f.ast.List()); got != 1 {
		t.Fatalf("alerts after raise = %d, want 1", got)
	}
	// A raise fires no lifecycle event and writes no marker line, so once
	// the fan-out barrier above holds these are settled at zero; observe
	// them through the same helper as every other count read in this test.
	waitForCount(t, "queued events after the first raise", 10*time.Second, func() int { return len(f.queue.all()) }, 0)
	if got := len(f.queue.all()); got != 0 {
		t.Fatalf("queued events after raise = %d, want 0", got)
	}
	waitForCount(t, "marker lines after the first raise", 10*time.Second, func() int { return markerLineCount(t, f.path) }, 0)
	if got := markerLineCount(t, f.path); got != 0 {
		t.Fatalf("marker lines after raise = %d, want 0", got)
	}

	// Answer → exactly one question.answered event and one marker line.
	rr := doQ(t, f.s, http.MethodPost, "/api/questions/"+id+"/answer", qstep2Answer())
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer (on) → %d (%s), want 201", rr.Code, rr.Body.String())
	}
	if evs := f.queue.waitFor(t, 1, "question.answered"); len(evs) != 1 || evs[len(evs)-1].Kind != autonomous.KindQuestionAnswered {
		t.Fatalf("events after answer = %v, want exactly one %q", evs, autonomous.KindQuestionAnswered)
	}
	waitForCount(t, "marker lines after the answer", 10*time.Second, func() int { return markerLineCount(t, f.path) }, 1)
	if got := markerLineCount(t, f.path); got != 1 {
		t.Fatalf("marker lines after answer = %d, want 1", got)
	}
	if got := qstep2CountOf(t, f, autonomous.KindQuestionAnswered); got != 1 {
		t.Fatalf("%q events = %d, want 1", autonomous.KindQuestionAnswered, got)
	}

	// Withdraw a second question → exactly one question.withdrawn, two
	// marker lines total, one event of each kind.
	// Every async side effect is read behind a wait: the second raise's
	// fan-out runs in its own goroutine, subscriber delivery of the
	// lifecycle events is on the subscriber's goroutine, and the store
	// appends its marker line after it fires the event — so an immediate
	// read right after the 200 can still be one behind.
	id2 := qstep2RaiseID(t, requireRaise(t, f))
	rr = doQ(t, f.s, http.MethodPost, "/api/questions/"+id2+"/withdraw", qstep2Withdraw())
	if rr.Code != http.StatusOK {
		t.Fatalf("withdraw (on) → %d (%s), want 200", rr.Code, rr.Body.String())
	}
	waitForCount(t, "queued events after the withdraw", 10*time.Second, func() int { return len(f.queue.all()) }, 2)
	if got := f.queue.waitFor(t, 2, "question.withdrawn"); len(got) != 2 {
		t.Fatalf("events after withdraw = %v, want one answered + one withdrawn", got)
	}
	if got := qstep2CountOf(t, f, autonomous.KindQuestionAnswered); got != 1 {
		t.Fatalf("%q events = %d, want 1", autonomous.KindQuestionAnswered, got)
	}
	if got := qstep2CountOf(t, f, autonomous.KindQuestionWithdrawn); got != 1 {
		t.Fatalf("%q events = %d, want 1", autonomous.KindQuestionWithdrawn, got)
	}
	waitForCount(t, "marker lines after the withdraw", 10*time.Second, func() int { return markerLineCount(t, f.path) }, 2)
	if got := markerLineCount(t, f.path); got != 2 {
		t.Fatalf("marker lines = %d, want 2 (one per terminal op)", got)
	}
	// The second raise's fan-out (router send, then alert) may still run
	// in its own goroutine: wait for it before reading, then assert as
	// before.
	waitForCount(t, "router sends", 10*time.Second, f.notifier.CallCount, 2)
	if got := f.notifier.CallCount(); got != 2 {
		t.Fatalf("router sends = %d, want 2 (one per raise)", got)
	}
	waitForCount(t, "alerts", 10*time.Second, func() int { return len(f.ast.List()) }, 2)
	if got := len(f.ast.List()); got != 2 {
		t.Fatalf("alerts = %d, want 2 (one per high-priority raise)", got)
	}
}

// TestQuestionStep2SwitchFlipOff_DropsEventSink: flipping the switch on
// and then off through PUT /api/config drops the sink again, so an answer
// recorded on the store afterwards fires no event and writes no marker.
func TestQuestionStep2SwitchFlipOff_DropsEventSink(t *testing.T) {
	f := qstep2New(t, false)
	putJSON(t, f.s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": true})
	putJSON(t, f.s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": false})

	id := qstep2Seed(t, f).ID
	if _, err := f.store.Answer(id, autonomous.Answer{ChosenOptionID: "yes", Actor: "operator"}); err != nil {
		t.Fatalf("store answer: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(f.queue.all()); got != 0 {
		t.Fatalf("events after flip off = %d, want 0", got)
	}
	if got := markerLineCount(t, f.path); got != 0 {
		t.Fatalf("marker lines after flip off = %d, want 0", got)
	}
}

// qstep2Body / qstep2Answer / qstep2Withdraw are the canonical step-2
// request bodies.
func qstep2Body() map[string]any {
	b := raiseBody(autonomous.QuestionPriorityHigh)
	b["session_id"] = "sess-qstep2"
	return b
}

func qstep2Answer() map[string]any {
	return map[string]any{"chosen_option_id": "yes", "quote": "go with yes", "actor": "operator"}
}

func qstep2Withdraw() map[string]any {
	return map[string]any{"reason": "no longer needed", "actor": "operator"}
}

// requireRaise POSTs a question through the real handler and fails the
// test unless it got a 201.
func requireRaise(t *testing.T, f *qstep2Fix) *httptest.ResponseRecorder {
	t.Helper()
	rr := doQ(t, f.s, http.MethodPost, "/api/questions", qstep2Body())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/questions (on) → %d (%s), want 201", rr.Code, rr.Body.String())
	}
	return rr
}
