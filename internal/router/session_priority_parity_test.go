package router

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func priorityRouter(t *testing.T) *Router {
	t.Helper()
	sm, err := session.NewManager("h", t.TempDir(), "echo", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	sm.WithFakeTmux()
	return &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
		manager:  sm,
	}
}

func TestSessionPriorityParseComm(t *testing.T) {
	cmd := Parse("new: priority=7: do the thing")
	if cmd.Type != CmdNew || !cmd.HasPriority || cmd.Priority != 7 || cmd.PriorityErr != "" || cmd.Text != "do the thing" {
		t.Fatalf("new parse = %+v", cmd)
	}
	cmd = Parse("new: llm=ollama priority=-2: task")
	if cmd.LLMRef != "ollama" || cmd.Priority != -2 || !cmd.HasPriority {
		t.Fatalf("new with llm parse = %+v", cmd)
	}
	cmd = Parse("new: priority=1.5: task")
	if cmd.Type != CmdNew || cmd.PriorityErr != session.ErrPriorityNotInteger.Error() {
		t.Fatalf("non-integer new parse = %+v", cmd)
	}
	cmd = Parse("priority ab12 9")
	if cmd.Type != CmdPriority || cmd.SessionID != "ab12" || !cmd.HasPriority || cmd.Priority != 9 {
		t.Fatalf("priority parse = %+v", cmd)
	}
}

// The same priority given to the comm-channel new and priority verbs is the
// value stored on the session.
func TestSessionPriorityParityComm(t *testing.T) {
	r := priorityRouter(t)
	resp := r.HandleTestMessage("new: priority=7: hello")
	if len(resp) == 0 {
		t.Fatal("no response to new")
	}
	var started *session.Session
	for _, s := range r.manager.ListSessions() {
		started = s
	}
	if started == nil {
		t.Fatalf("no session started; responses: %v", resp)
	}
	t.Cleanup(func() { _ = r.manager.Delete(started.FullID, false) })
	if started.Priority != 7 {
		t.Fatalf("new stored priority = %d, want 7", started.Priority)
	}

	resp = r.HandleTestMessage("priority " + started.FullID + " 9")
	if len(resp) == 0 || !strings.Contains(resp[0], "Priority set to 9") {
		t.Fatalf("priority response = %v", resp)
	}
	if got, _ := r.manager.GetSession(started.FullID); got == nil || got.Priority != 9 {
		t.Fatalf("priority verb stored %+v, want 9", got)
	}
}

func TestSessionPriorityCommRejectsNonInteger(t *testing.T) {
	r := priorityRouter(t)
	resp := r.HandleTestMessage("new: priority=abc: hello")
	if len(resp) == 0 || !strings.Contains(resp[0], session.ErrPriorityNotInteger.Error()) {
		t.Fatalf("new response = %v", resp)
	}
	if n := len(r.manager.ListSessions()); n != 0 {
		t.Fatalf("rejected new created %d sessions", n)
	}
	resp = r.HandleTestMessage("priority aa11 1.5")
	if len(resp) == 0 || !strings.Contains(resp[0], session.ErrPriorityNotInteger.Error()) {
		t.Fatalf("priority response = %v", resp)
	}
}
