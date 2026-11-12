package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// The same priority given to start_session and set_session_priority is the
// value stored on the session.
func TestSessionPriorityParityMCP(t *testing.T) {
	s := bl91Server(t)
	ctx := context.Background()

	res, err := s.handleStartSession(ctx, call(map[string]any{"task": "hello", "project_dir": t.TempDir(), "priority": float64(7)}))
	out := resultText(t, res, err)
	if res.IsError {
		t.Fatalf("start_session: %s", out)
	}
	var started *session.Session
	for _, sess := range s.manager.ListSessions() {
		started = sess
	}
	if started == nil {
		t.Fatal("no session started")
	}
	t.Cleanup(func() { _ = s.manager.Delete(started.FullID, false) })
	if started.Priority != 7 {
		t.Fatalf("start_session stored priority = %d, want 7", started.Priority)
	}

	res, err = s.handleSetSessionPriority(ctx, call(map[string]any{"session_id": started.FullID, "priority": float64(9)}))
	if out := resultText(t, res, err); res.IsError {
		t.Fatalf("set_session_priority: %s", out)
	}
	if got, _ := s.manager.GetSession(started.FullID); got == nil || got.Priority != 9 {
		t.Fatalf("set_session_priority stored %+v, want 9", got)
	}
}

func TestSessionPriorityMCPRejectsNonInteger(t *testing.T) {
	s := bl91Server(t)
	ctx := context.Background()
	for _, bad := range []any{1.5, "abc", true} {
		res, err := s.handleStartSession(ctx, call(map[string]any{"task": "hello", "priority": bad}))
		if out := resultText(t, res, err); !res.IsError || !strings.Contains(out, session.ErrPriorityNotInteger.Error()) {
			t.Errorf("start_session priority %v: %q", bad, out)
		}
		res, err = s.handleSetSessionPriority(ctx, call(map[string]any{"session_id": "x", "priority": bad}))
		if out := resultText(t, res, err); !res.IsError || !strings.Contains(out, session.ErrPriorityNotInteger.Error()) {
			t.Errorf("set_session_priority priority %v: %q", bad, out)
		}
	}
	if n := len(s.manager.ListSessions()); n != 0 {
		t.Fatalf("rejected calls created %d sessions", n)
	}
}
