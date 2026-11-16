// Quick reply question routing tests: a send-to-session reply answers the
// session's open question instead of typing when it means an answer.

package router

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// replyLog collects the router's asynchronous channel sends.
type replyLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *replyLog) add(m string) { l.mu.Lock(); l.msgs = append(l.msgs, m); l.mu.Unlock() }

// waitLast waits briefly for a send and returns the latest one.
func (l *replyLog) waitLast() string {
	for i := 0; i < 200; i++ {
		l.mu.Lock()
		n := len(l.msgs)
		var last string
		if n > 0 {
			last = l.msgs[n-1]
		}
		l.mu.Unlock()
		if n > 0 {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func quickReplyRouter(t *testing.T, f *questionFixture) (*Router, *session.FakeTmux, *replyLog) {
	t.Helper()
	port, closeFn := fakeQuestionDaemon(t, f)
	t.Cleanup(closeFn)
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(sm)
	trackSessionManager(t, sm)
	ft := sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	now := time.Now()
	if err := sm.SaveSession(&session.Session{
		ID: "qr01", FullID: "h-qr01", Hostname: "h", TmuxSession: "cs-qr01",
		Task: "quick reply", State: session.StateWaitingInput, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	sent := &replyLog{}
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "signal", capture: sent.add},
		manager:  sm,
	}
	r.SetWebPort(port)
	return r, ft, sent
}

func sessionQuestion(id, sessionID string, opts ...map[string]any) map[string]any {
	q := questionQuestion(id, "Ship?", "Ship the change?", opts...)
	q["session_id"] = sessionID
	return q
}

func typedCount(ft *session.FakeTmux) int {
	n := 0
	for _, c := range ft.CallsSnapshot() {
		if strings.HasPrefix(c.Op, "send-keys") {
			n++
		}
	}
	return n
}

func TestQuickReplyQuestionAnswered(t *testing.T) {
	f := &questionFixture{open: []map[string]any{
		sessionQuestion("q_1", "qr01", questionOption("yes", "Yes"), questionOption("no", "No")),
	}}
	r, ft, sent := quickReplyRouter(t, f)
	r.handleSend(Command{Type: CmdSend, SessionID: "qr01", Text: "Approved"})

	last := sent.waitLast()
	rec := f.lastAnswer("q_1")
	if rec == nil {
		t.Fatalf("no answer recorded; reply: %q", last)
	}
	if rec.ChosenOptionID != "yes" {
		t.Errorf("option = %q, want yes", rec.ChosenOptionID)
	}
	if !strings.HasPrefix(rec.Surface, "comm:") {
		t.Errorf("surface = %q, want comm:<channel>", rec.Surface)
	}
	if n := typedCount(ft); n != 0 {
		t.Errorf("text was typed into the session (%d calls)", n)
	}
	if !strings.Contains(last, "Yes") || !strings.Contains(last, "Approved") {
		t.Errorf("reply should name the option and what was sent: %q", last)
	}
}

func TestQuickReplyNoQuestionTypesText(t *testing.T) {
	f := &questionFixture{open: []map[string]any{
		sessionQuestion("q_9", "other", questionOption("yes", "Yes")),
	}}
	r, ft, sent := quickReplyRouter(t, f)
	r.handleSend(Command{Type: CmdSend, SessionID: "qr01", Text: "yes"})

	last := sent.waitLast()
	if len(f.answers) != 0 {
		t.Errorf("answer posted for another session's question: %+v", f.answers)
	}
	if typedCount(ft) == 0 || !strings.Contains(last, "Input sent") {
		t.Errorf("text was not typed; reply: %q", last)
	}
}

func TestQuickReplyNoMatchingOptionRefused(t *testing.T) {
	f := &questionFixture{open: []map[string]any{
		sessionQuestion("q_2", "h-qr01", questionOption("opt_a", "Ship it"), questionOption("opt_b", "Wait")),
	}}
	r, ft, sent := quickReplyRouter(t, f)
	r.handleSend(Command{Type: CmdSend, SessionID: "qr01", Text: "yes"})
	last := sent.waitLast()

	if len(f.answers) != 0 {
		t.Errorf("answer posted: %+v", f.answers)
	}
	for _, q := range f.reqs {
		if q.method == "POST" {
			t.Errorf("unexpected POST %s", q.path)
		}
	}
	if n := typedCount(ft); n != 0 {
		t.Errorf("text typed (%d calls)", n)
	}
	if !strings.Contains(last, "refused") || !strings.Contains(last, "Ship it, Wait") {
		t.Errorf("refusal message = %q", last)
	}
}
