// BL354 — Name-addressed session operations: tests for resolveSession tie-breaking
// and session_name param on MCP tool handlers.

package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// TestBL354_ResolveSession_TieBreak verifies that when two active sessions share
// the same name, resolveSession returns the OLDER one (earliest CreatedAt) rather
// than returning an ambiguity error.
func TestBL354_ResolveSession_TieBreak(t *testing.T) {
	s := bl91Server(t)

	older := time.Now().Add(-10 * time.Minute)
	newer := time.Now()

	// Two running sessions with the same name.
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "aa01", FullID: "testhost-aa01", Hostname: "testhost",
		Name: "shared-name", Task: "task A", State: session.StateRunning,
		CreatedAt: older, UpdatedAt: older,
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "aa02", FullID: "testhost-aa02", Hostname: "testhost",
		Name: "shared-name", Task: "task B", State: session.StateRunning,
		CreatedAt: newer, UpdatedAt: newer,
	})

	resolved, err := s.resolveSession("shared-name")
	if err != nil {
		t.Fatalf("expected no error during tie-break, got: %v", err)
	}
	if resolved == nil {
		t.Fatal("expected a session, got nil")
	}
	if resolved.ID != "aa01" {
		t.Errorf("tie-break should return OLDER session (aa01), got %q", resolved.ID)
	}
}

// TestBL354_ResolveSession_TieBreak_ThreeSessions verifies ordering with three
// active sessions sharing a name — the oldest is always returned.
func TestBL354_ResolveSession_TieBreak_ThreeSessions(t *testing.T) {
	s := bl91Server(t)

	t0 := time.Now().Add(-20 * time.Minute)
	t1 := time.Now().Add(-10 * time.Minute)
	t2 := time.Now()

	for _, tc := range []struct {
		id        string
		createdAt time.Time
	}{
		{"bb02", t1},
		{"bb01", t0}, // oldest — should win
		{"bb03", t2},
	} {
		s.manager.SaveSession(&session.Session{ //nolint:errcheck
			ID: tc.id, FullID: "testhost-" + tc.id, Hostname: "testhost",
			Name: "multi-name", Task: "task", State: session.StateRunning,
			CreatedAt: tc.createdAt, UpdatedAt: tc.createdAt,
		})
	}

	resolved, err := s.resolveSession("multi-name")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resolved == nil {
		t.Fatal("expected a session, got nil")
	}
	if resolved.ID != "bb01" {
		t.Errorf("expected oldest session bb01, got %q", resolved.ID)
	}
}

// TestBL354_SendInput_BySessionName verifies that send_input resolves when
// session_name is provided and session_id is empty.
func TestBL354_SendInput_BySessionName(t *testing.T) {
	s := bl91Server(t)

	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "si01", FullID: "testhost-si01", Hostname: "testhost",
		Name: "input-target", Task: "waiting task", State: session.StateWaitingInput,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	// session_id empty, session_name provided — should resolve and find the session.
	// Because the fake tmux doesn't have a real pane, SendInput will fail with a
	// non-session-lookup error (e.g. "session not found" on the FullID path) but
	// crucially it must NOT say "session_id or session_name is required".
	res := invoke(t, s.handleSendInput, map[string]any{
		"session_name": "input-target",
		"text":         "hello",
	})
	if strings.Contains(res, "session_id or session_name is required") {
		t.Errorf("should have resolved past the empty-id guard, got: %q", res)
	}
}

// TestBL354_KillSession_BySessionName verifies that kill_session works with
// session_name instead of session_id.
func TestBL354_KillSession_BySessionName(t *testing.T) {
	s := bl91Server(t)

	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ks01", FullID: "testhost-ks01", Hostname: "testhost",
		Name: "kill-by-name", Task: "task", State: session.StateRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res := invoke(t, s.handleKillSession, map[string]any{
		"session_name": "kill-by-name",
	})
	// Should not produce the "session_id or session_name is required" guard error.
	if strings.Contains(res, "session_id or session_name is required") {
		t.Errorf("should have resolved past the empty-id guard, got: %q", res)
	}
}

// TestBL354_SessionOutput_BySessionName verifies session_output resolves by name.
func TestBL354_SessionOutput_BySessionName(t *testing.T) {
	s := bl91Server(t)

	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "so01", FullID: "testhost-so01", Hostname: "testhost",
		Name: "output-by-name", Task: "task", State: session.StateRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})

	res := invoke(t, s.handleSessionOutput, map[string]any{
		"session_name": "output-by-name",
	})
	// Should resolve the session (may error on tail output, but not on session lookup).
	if strings.Contains(res, "session_id or session_name is required") {
		t.Errorf("should have resolved past the empty-id guard, got: %q", res)
	}
}

// TestBL354_MissingBothIDs verifies that omitting both session_id and session_name
// returns the correct validation error.
func TestBL354_MissingBothIDs(t *testing.T) {
	s := bl91Server(t)

	for name, fn := range map[string]func(map[string]any) string{
		"send_input": func(args map[string]any) string {
			args["text"] = "hi"
			return invoke(t, s.handleSendInput, args)
		},
		"kill_session":    func(args map[string]any) string { return invoke(t, s.handleKillSession, args) },
		"session_output":  func(args map[string]any) string { return invoke(t, s.handleSessionOutput, args) },
		"session_timeline": func(args map[string]any) string { return invoke(t, s.handleSessionTimeline, args) },
		"session_children": func(args map[string]any) string { return invoke(t, s.handleSessionChildren, args) },
	} {
		res := fn(map[string]any{})
		if !strings.Contains(res, "session_id or session_name is required") {
			t.Errorf("[%s] want validation error, got: %q", name, res)
		}
	}
}
