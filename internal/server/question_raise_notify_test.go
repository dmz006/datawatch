// question_raise_notify_test.go — the notification fan-out on a
// successful POST /api/questions: the hub receives exactly one
// question_raised frame, the router (notifier seam) receives exactly
// one call, the alert fires only for high-priority questions, each
// notify_* toggle suppresses its own leg, the autonomous.questions
// switch-off suppresses every part (the raise itself is refused too),
// and a blocking or failing notifier cannot hold the raise.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// questionNotifyFake stands in for the messaging fan-out main.go wires.
// block (non-nil and open) makes it hang until the test closes it — the
// "slow or failing notifier" in the last test.
type questionNotifyFake struct {
	mu    sync.Mutex
	block chan struct{}
	calls int
}

func (f *questionNotifyFake) NotifyQuestionRaised(autonomous.Question) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
}

func (f *questionNotifyFake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newQuestionNotifyServer builds a Server with the channel on (per test
// qc), a live Hub, an in-memory-backed alert store and the fake
// question store (Raise assigns the id "q-fake-raised").
func newQuestionNotifyServer(t *testing.T, qc config.AutonomousQuestionsConfig) (*Server, *Hub, *alerts.Store) {
	t.Helper()
	cfg := &config.Config{Autonomous: config.AutonomousConfig{Questions: qc}}
	hub := NewHub()
	s := NewServer(hub, nil, "dw", "", nil, cfg, filepath.Join(t.TempDir(), "config.json"))
	ast, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatalf("alerts store: %v", err)
	}
	s.alertStore = ast
	s.SetQuestions(newFakeQuestions())
	return s, hub, ast
}

// raiseBody builds a valid raise body; priority is added only when set.
func raiseBody(priority string) map[string]any {
	body := map[string]any{
		"label":   "decision",
		"text":    "Cap the retry backoff at five minutes?",
		"context": "none",
		"default": "yes",
		"options": []map[string]any{
			{"id": "yes", "label": "Yes"},
			{"id": "no", "label": "No"},
		},
	}
	if priority != "" {
		body["priority"] = priority
	}
	return body
}

// postRaisedQuestion POSTs one question and requires a 201 raise.
func postRaisedQuestion(t *testing.T, s *Server, priority string) {
	t.Helper()
	rr := doQ(t, s, http.MethodPost, "/api/questions", raiseBody(priority))
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/questions → %d (%s), want 201", rr.Code, rr.Body.String())
	}
}

// waitTrue polls fn until true or timeout (the fan-out runs in its own
// goroutine, so assertions wait rather than assume an instant).
func waitTrue(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// drainHubFrames settles, then non-blockingly drains the hub broadcast
// channel and decodes each frame (NewHub gives it 256 slots and the
// test never runs the hub loop, so draining reads the queued frames).
func drainHubFrames(t *testing.T, h *Hub, settle time.Duration) []WSMessage {
	t.Helper()
	if settle > 0 {
		time.Sleep(settle)
	}
	var frames []WSMessage
	for {
		select {
		case m := <-h.broadcast:
			var wm WSMessage
			if err := json.Unmarshal(m.payload, &wm); err != nil {
				t.Fatalf("hub frame does not decode: %v", err)
			}
			frames = append(frames, wm)
		default:
			return frames
		}
	}
}

func countFrames(frames []WSMessage, want MessageType) int {
	n := 0
	for _, f := range frames {
		if f.Type == want {
			n++
		}
	}
	return n
}

func mustDecodeFrameData(t *testing.T, got []WSMessage, want MessageType, into *QuestionRaisedData) {
	t.Helper()
	for _, f := range got {
		if f.Type != want {
			continue
		}
		if err := json.Unmarshal(f.Data, into); err != nil {
			t.Fatalf("question_raised frame payload does not decode: %v", err)
		}
		return
	}
	t.Fatalf("no %q frame in hub frames", want)
}

// TestQuestionRaiseNotify_HubAndRouterEachCalledOnce — the success path:
// the hub gets exactly one question_raised frame with the question's
// content, the router seam exactly one call, and no alert (the
// question is not high-priority).
func TestQuestionRaiseNotify_HubAndRouterEachCalledOnce(t *testing.T) {
	s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{Enabled: true})
	fake := &questionNotifyFake{}
	s.SetQuestionRaiseNotifier(fake)

	postRaisedQuestion(t, s, "")

	waitTrue(t, 2*time.Second, func() bool { return fake.CallCount() == 1 })
	if got := fake.CallCount(); got != 1 {
		t.Fatalf("router seam called %d times, want exactly one", got)
	}

	frames := drainHubFrames(t, hub, 150*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("hub got %d question_raised frame(s), want exactly one (frames: %v)", got, frames)
	}
	var data QuestionRaisedData
	mustDecodeFrameData(t, frames, MsgQuestionRaised, &data)
	if data.ID != "q-fake-raised" || data.Text == "" || len(data.Options) != 2 {
		t.Fatalf("question_raised frame content wrong: %+v", data)
	}

	if got := len(ast.List()); got != 0 {
		t.Fatalf("alert store has %d alert(s) for a non-high question, want 0", got)
	}
}

// TestQuestionRaiseNotify_AlertOnlyWhenHighPriority — the alert leg is
// specific to high-priority questions: none for normal, exactly one
// (plus its WS alert frame) for high.
func TestQuestionRaiseNotify_AlertOnlyWhenHighPriority(t *testing.T) {
	t.Run("normal priority, no alert", func(t *testing.T) {
		s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{Enabled: true})
		postRaisedQuestion(t, s, "")
		time.Sleep(150 * time.Millisecond)
		if got := len(ast.List()); got != 0 {
			t.Fatalf("alert store has %d alert(s) for normal priority, want 0", got)
		}
		if got := countFrames(drainHubFrames(t, hub, 100*time.Millisecond), MsgAlert); got != 0 {
			t.Fatalf("hub got %d alert frame(s) for normal priority, want 0", got)
		}
	})

	t.Run("high priority, one alert", func(t *testing.T) {
		s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{Enabled: true})
		postRaisedQuestion(t, s, "high")

		waitTrue(t, 2*time.Second, func() bool { return len(ast.List()) == 1 })
		list := ast.List()
		if len(list) != 1 {
			t.Fatalf("alert store has %d alert(s) for high priority, want exactly one", len(list))
		}
		if !strings.Contains(list[0].Title, "q-fake-raised") {
			t.Fatalf("alert title %q does not name the question id", list[0].Title)
		}

		frames := drainHubFrames(t, hub, 150*time.Millisecond)
		if got := countFrames(frames, MsgAlert); got != 1 {
			t.Fatalf("hub got %d alert frame(s), want exactly one", got)
		}
		if got := countFrames(frames, MsgQuestionRaised); got != 1 {
			t.Fatalf("hub got %d question_raised frame(s), want exactly one (the WS leg is independent of the alert leg)", got)
		}
	})
}

// TestQuestionRaiseNotify_MessagingToggleOffSuppressesRoute —
// notify_messaging false silences the router leg and nothing else: the
// hub frame still lands, and (for a high-priority question) the alert
// leg still fires.
func TestQuestionRaiseNotify_MessagingToggleOffSuppressesRoute(t *testing.T) {
	off := false
	s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{
		Enabled:         true,
		NotifyMessaging: &off,
	})
	fake := &questionNotifyFake{}
	s.SetQuestionRaiseNotifier(fake)

	postRaisedQuestion(t, s, "high")
	time.Sleep(200 * time.Millisecond)

	if got := fake.CallCount(); got != 0 {
		t.Fatalf("router seam called %d time(s) with notify_messaging off, want 0", got)
	}
	frames := drainHubFrames(t, hub, 100*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("hub got %d question_raised frame(s) with notify_messaging off, want one (WS leg is independent)", got)
	}
	if got := len(ast.List()); got != 1 {
		t.Fatalf("alert store has %d alert(s) with notify_messaging off, want one (alert leg is independent)", got)
	}
}

// TestQuestionRaiseNotify_AlertToggleOffSuppressesAlert — notify_alert
// false silences the alert leg on a high-priority question; the other
// legs still fire.
func TestQuestionRaiseNotify_AlertToggleOffSuppressesAlert(t *testing.T) {
	off := false
	s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{
		Enabled:     true,
		NotifyAlert: &off,
	})
	fake := &questionNotifyFake{}
	s.SetQuestionRaiseNotifier(fake)

	postRaisedQuestion(t, s, "high")

	waitTrue(t, 2*time.Second, func() bool { return fake.CallCount() == 1 })
	time.Sleep(150 * time.Millisecond)

	if got := len(ast.List()); got != 0 {
		t.Fatalf("alert store has %d alert(s) with notify_alert off, want 0", got)
	}
	frames := drainHubFrames(t, hub, 100*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("hub got %d question_raised frame(s) with notify_alert off, want one (WS leg is independent)", got)
	}
}

// TestQuestionRaiseNotify_SwitchOffSuppressesEverything — with
// autonomous.questions.enabled false the raise is refused AND the fan-out
// is gone: no router call, no hub frame, no alert.
func TestQuestionRaiseNotify_SwitchOffSuppressesEverything(t *testing.T) {
	off := false
	s, hub, ast := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{
		Enabled:         off,
		NotifyMessaging: nil,
		NotifyAlert:     nil,
	})
	fake := &questionNotifyFake{}
	s.SetQuestionRaiseNotifier(fake)

	rr := doQ(t, s, http.MethodPost, "/api/questions", raiseBody("high"))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("switch-off raise → %d (%s), want 503 (channel closed at the handler)", rr.Code, rr.Body.String())
	}
	time.Sleep(150 * time.Millisecond)

	if got := fake.CallCount(); got != 0 {
		t.Fatalf("router seam called %d time(s) with the switch off, want 0", got)
	}
	if frames := drainHubFrames(t, hub, 100*time.Millisecond); len(frames) != 0 {
		t.Fatalf("hub got %d frame(s) with the switch off, want none", len(frames))
	}
	if got := len(ast.List()); got != 0 {
		t.Fatalf("alert store has %d alert(s) with the switch off, want 0", got)
	}
}

// TestQuestionRaiseNotify_BlockingNotifierDoesNotBlockRaise — the raise
// is the thing; the notification must not make it slow. A notifier that
// hangs must not hold POST /api/questions: the 201 returns quickly, and
// the (eventual) single call to the notifier and the hub frame still
// happen once it is released.
func TestQuestionRaiseNotify_BlockingNotifierDoesNotBlockRaise(t *testing.T) {
	s, hub, _ := newQuestionNotifyServer(t, config.AutonomousQuestionsConfig{Enabled: true})
	fake := &questionNotifyFake{block: make(chan struct{})}
	s.SetQuestionRaiseNotifier(fake)

	type result struct {
		code int
	}
	ch := make(chan result, 1)
	start := time.Now()
	go func() {
		b, _ := json.Marshal(raiseBody("high"))
		req := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		s.handleQuestions(rr, req) //nolint:errcheck
		ch <- result{code: rr.Code}
	}()

	select {
	case res := <-ch:
		elapsed := time.Since(start)
		if res.code != http.StatusCreated {
			t.Fatalf("POST /api/questions → %d, want 201", res.code)
		}
		if elapsed > 750*time.Millisecond {
			t.Fatalf("raise took %v behind a hanging notifier; the fan-out must be off the request path", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST /api/questions never returned: a blocking or failing notifier must not hold the raise")
	}

	// Once the notifier is released, the call still happens exactly once
	// and the WS leg still lands — the raise's notification is best-effort
	// but not dropped.
	close(fake.block)
	waitTrue(t, 2*time.Second, func() bool { return fake.CallCount() == 1 })
	if got := fake.CallCount(); got != 1 {
		t.Fatalf("router seam called %d time(s) after release, want exactly one", got)
	}
	frames := drainHubFrames(t, hub, 150*time.Millisecond)
	if got := countFrames(frames, MsgQuestionRaised); got != 1 {
		t.Fatalf("hub got %d question_raised frame(s) after release, want exactly one", got)
	}
}
