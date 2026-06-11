// BL341 — GAP 1: send_input name resolution + GAP 2: start_session permission_mode.

package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// ── resolveSession ────────────────────────────────────────────────────────────

func TestResolveSession_ByShortID(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ab01", FullID: "testhost-ab01", Hostname: "testhost",
		Task: "task", State: session.StateRunning, UpdatedAt: time.Now(),
	})
	sess, err := s.resolveSession("ab01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil || sess.ID != "ab01" {
		t.Fatalf("want ab01, got %v", sess)
	}
}

func TestResolveSession_ByName_Single(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cd02", FullID: "testhost-cd02", Hostname: "testhost",
		Name: "my-worker", Task: "work", State: session.StateRunning,
		UpdatedAt: time.Now(),
	})
	sess, err := s.resolveSession("my-worker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil || sess.ID != "cd02" {
		t.Fatalf("want cd02, got %v", sess)
	}
}

func TestResolveSession_ByName_NotFound(t *testing.T) {
	s := bl91Server(t)
	sess, err := s.resolveSession("no-such-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil, got %v", sess)
	}
}

// TestResolveSession_ByName_MultipleActive_TieBreak verifies BL354 tie-breaking:
// when multiple active sessions share a name, the OLDEST (earliest CreatedAt) is
// returned instead of an ambiguity error.
func TestResolveSession_ByName_MultipleActive_TieBreak(t *testing.T) {
	s := bl91Server(t)
	older := time.Now().Add(-5 * time.Minute)
	newer := time.Now()
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ef04", FullID: "testhost-ef04", Hostname: "testhost",
		Name: "shared-name", Task: "t", State: session.StateRunning,
		CreatedAt: newer, UpdatedAt: newer,
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ef03", FullID: "testhost-ef03", Hostname: "testhost",
		Name: "shared-name", Task: "t", State: session.StateRunning,
		CreatedAt: older, UpdatedAt: older,
	})
	sess, err := s.resolveSession("shared-name")
	if err != nil {
		t.Fatalf("BL354: expected no error during tie-break, got: %v", err)
	}
	if sess == nil {
		t.Fatal("expected a session, got nil")
	}
	// ef03 is older — it should win.
	if sess.ID != "ef03" {
		t.Errorf("BL354: expected oldest session ef03, got %q", sess.ID)
	}
}

func TestResolveSession_ByName_MultipleSameName_OnlyOneActive(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "gh05", FullID: "testhost-gh05", Hostname: "testhost",
		Name: "proj", Task: "old", State: session.StateComplete,
		UpdatedAt: time.Now().Add(-time.Hour),
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "gh06", FullID: "testhost-gh06", Hostname: "testhost",
		Name: "proj", Task: "new", State: session.StateRunning,
		UpdatedAt: time.Now(),
	})
	sess, err := s.resolveSession("proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil || sess.ID != "gh06" {
		t.Fatalf("want active session gh06, got %v", sess)
	}
}

func TestResolveSession_ByName_MultipleAllDone_PicksMostRecent(t *testing.T) {
	s := bl91Server(t)
	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-time.Minute)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ij07", FullID: "testhost-ij07", Hostname: "testhost",
		Name: "archive", State: session.StateComplete, UpdatedAt: old,
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "ij08", FullID: "testhost-ij08", Hostname: "testhost",
		Name: "archive", State: session.StateComplete, UpdatedAt: recent,
	})
	sess, err := s.resolveSession("archive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil || sess.ID != "ij08" {
		t.Fatalf("want most recent ij08, got %v", sess)
	}
}

// ── handleSendInput name resolution ──────────────────────────────────────────

func TestSendInput_ByName(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "kl09", FullID: "testhost-kl09", Hostname: "testhost",
		Name: "test-session", Task: "wait", State: session.StateWaitingInput,
		TmuxSession: "testhost-kl09", UpdatedAt: time.Now(),
	})
	text := invoke(t, s.handleSendInput,
		map[string]any{"session_id": "test-session", "text": "ping"})
	// FakeTmux may or may not error; the important thing is we didn't get "not found".
	if strings.Contains(text, "not found") {
		t.Errorf("session should be found by name, got: %q", text)
	}
}

// TestSendInput_ByName_TwoActive_RouteToOldest verifies BL354: when two active
// sessions share a name, send_input routes to the oldest instead of failing.
func TestSendInput_ByName_TwoActive_RouteToOldest(t *testing.T) {
	s := bl91Server(t)
	older := time.Now().Add(-5 * time.Minute)
	newer := time.Now()
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "mn11", FullID: "testhost-mn11", Hostname: "testhost",
		Name: "dupe", State: session.StateRunning, CreatedAt: newer, UpdatedAt: newer,
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "mn10", FullID: "testhost-mn10", Hostname: "testhost",
		Name: "dupe", State: session.StateRunning, CreatedAt: older, UpdatedAt: older,
	})
	text := invoke(t, s.handleSendInput,
		map[string]any{"session_id": "dupe", "text": "hi"})
	// Should NOT produce an ambiguity error — either succeeds or fails on tmux,
	// but must not return "multiple active".
	if strings.Contains(strings.ToLower(text), "multiple active") {
		t.Errorf("BL354: should not produce ambiguity error, got: %q", text)
	}
	// Should reference the older session (mn10), not the newer one.
	if strings.Contains(text, "mn11") && !strings.Contains(text, "mn10") {
		t.Errorf("BL354: expected routing to mn10 (older), got: %q", text)
	}
}

func TestSendInput_ByName_NotFound(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleSendInput,
		map[string]any{"session_id": "ghost-session", "text": "hello"})
	if !strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("expected not-found, got: %q", text)
	}
}

// ── handleStartSession permission_mode ───────────────────────────────────────

func TestStartSession_PermissionMode_Valid(t *testing.T) {
	s := bl91Server(t)
	for _, pm := range []string{"default", "plan", "acceptEdits", "auto", "bypassPermissions", "dontAsk"} {
		text := invoke(t, s.handleStartSession,
			map[string]any{"task": "ls /", "permission_mode": pm})
		if strings.Contains(strings.ToLower(text), "invalid permission_mode") {
			t.Errorf("mode %q rejected as invalid, got: %q", pm, text)
		}
	}
}

func TestStartSession_PermissionMode_Invalid(t *testing.T) {
	s := bl91Server(t)
	text := invoke(t, s.handleStartSession,
		map[string]any{"task": "ls /", "permission_mode": "superDangerous"})
	if !strings.Contains(strings.ToLower(text), "invalid permission_mode") {
		t.Errorf("expected validation error, got: %q", text)
	}
}

func TestStartSession_PermissionMode_Empty_OK(t *testing.T) {
	s := bl91Server(t)
	// Empty permission_mode should not be rejected (uses config default).
	text := invoke(t, s.handleStartSession,
		map[string]any{"task": "ls /"})
	if strings.Contains(strings.ToLower(text), "invalid permission_mode") {
		t.Errorf("empty permission_mode should not error, got: %q", text)
	}
}
