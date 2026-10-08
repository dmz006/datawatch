// HLLM-004 — a spawned session's MCP access (either the "Goose channel"
// subprocess, with a static callerSessionID, or — the primary path — a
// session token through the shared daemon MCP server, with the real caller
// identity carried per-call on ctx via federation.WithCallerSessionID) used
// to have unscoped access to session_output/kill_session/stop_all_sessions/
// send_input for ANY session on the host (not just its own), and to
// memory_forget/memory_pin/memory_export regardless of which session owns
// the data. These tests pin the ownership-scoping fix for both paths.

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func mustSaveSession(t *testing.T, s *Server, id, parentID string) *session.Session {
	t.Helper()
	sess := &session.Session{
		ID: id, FullID: "testhost-" + id, Hostname: "testhost",
		Name: id, Task: "task", State: session.StateRunning,
		ParentID:  parentID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestOwnsSession_AdminMode_AlwaysTrue(t *testing.T) {
	s := bl91Server(t)
	other := mustSaveSession(t, s, "other", "")
	if !s.ownsSession(context.Background(), other.FullID) {
		t.Fatal("admin-mode (no callerSessionID) should own every session")
	}
}

func TestOwnsSession_Subprocess_SelfAndDescendants(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	child := mustSaveSession(t, s, "child", me.FullID)
	grandchild := mustSaveSession(t, s, "grandchild", child.FullID)
	unrelated := mustSaveSession(t, s, "unrelated", "")
	s.callerSessionID = me.FullID

	if !s.ownsSession(context.Background(), me.FullID) {
		t.Error("should own itself")
	}
	if !s.ownsSession(context.Background(), child.FullID) {
		t.Error("should own a direct child")
	}
	if !s.ownsSession(context.Background(), grandchild.FullID) {
		t.Error("should own a grandchild (transitive descendant)")
	}
	if s.ownsSession(context.Background(), unrelated.FullID) {
		t.Error("should NOT own an unrelated session")
	}
}

func TestHandleSessionOutput_Subprocess_DeniedForUnownedSession(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	other := mustSaveSession(t, s, "other", "")
	s.callerSessionID = me.FullID

	out, err := s.handleSessionOutput(context.Background(), call(map[string]any{"session_id": other.FullID}))
	res := resultText(t, out, err)
	if !strings.Contains(res, "not found") {
		t.Errorf("expected a not-found-style denial for an unowned session, got: %q", res)
	}
}

func TestHandleSessionOutput_Subprocess_AllowedForOwnChild(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	child := mustSaveSession(t, s, "child", me.FullID)
	s.callerSessionID = me.FullID

	out, err := s.handleSessionOutput(context.Background(), call(map[string]any{"session_id": child.FullID}))
	res := resultText(t, out, err)
	if strings.Contains(res, "not found") {
		t.Errorf("should be able to read own child's output, got: %q", res)
	}
}

func TestHandleKillSession_Subprocess_DeniedForUnownedSession(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	other := mustSaveSession(t, s, "other", "")
	s.callerSessionID = me.FullID

	out, err := s.handleKillSession(context.Background(), call(map[string]any{"session_id": other.FullID}))
	res := resultText(t, out, err)
	if !strings.Contains(res, "not found") {
		t.Errorf("expected a not-found-style denial for an unowned session, got: %q", res)
	}
	if got, _ := s.manager.GetSession(other.FullID); got.State == session.StateKilled {
		t.Error("unowned session must not actually be killed")
	}
}

func TestHandleSendInput_Subprocess_DeniedForUnownedSession(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	other := mustSaveSession(t, s, "other", "")
	s.callerSessionID = me.FullID

	out, err := s.handleSendInput(context.Background(), call(map[string]any{"session_id": other.FullID, "text": "hi"}))
	res := resultText(t, out, err)
	if !strings.Contains(res, "not found") {
		t.Errorf("expected a not-found-style denial for an unowned session, got: %q", res)
	}
}

func TestHandleStopAllSessions_Subprocess_OnlyOwnSubtree(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	child := mustSaveSession(t, s, "child", me.FullID)
	other := mustSaveSession(t, s, "other", "")
	s.callerSessionID = me.FullID

	if _, err := s.handleStopAllSessions(context.Background(), call(nil)); err != nil {
		t.Fatalf("handleStopAllSessions: %v", err)
	}

	childAfter, _ := s.manager.GetSession(child.FullID)
	otherAfter, _ := s.manager.GetSession(other.FullID)
	if childAfter.State != session.StateKilled {
		t.Errorf("own child should have been killed, state=%s", childAfter.State)
	}
	if otherAfter.State == session.StateKilled {
		t.Error("unrelated session must not be killed by a subprocess-scoped stop_all_sessions")
	}
}

func TestMemoryForgetPinExport_BlockedForScopedCaller(t *testing.T) {
	s := bl91Server(t)
	s.callerSessionID = "testhost-me"

	out1, err1 := s.handleMemoryForget(context.Background(), call(map[string]any{"id": 1}))
	if res := resultText(t, out1, err1); !strings.Contains(res, "not available for a scoped session credential") {
		t.Errorf("memory_forget should be blocked for a scoped caller, got: %q", res)
	}
	out2, err2 := s.handleMemoryPin(context.Background(), call(map[string]any{"id": 1}))
	if res := resultText(t, out2, err2); !strings.Contains(res, "not available for a scoped session credential") {
		t.Errorf("memory_pin should be blocked for a scoped caller, got: %q", res)
	}
	out3, err3 := s.handleMemoryExport(context.Background(), call(nil))
	if res := resultText(t, out3, err3); !strings.Contains(res, "not available for a scoped session credential") {
		t.Errorf("memory_export should be blocked for a scoped caller, got: %q", res)
	}
}

// ── Primary path: shared daemon MCP server, per-call ctx identity ──────────
// This is the real-world case: internal/server's handleMCPCall resolves the
// caller's own scoped session token and stashes the owning session ID on
// ctx (federation.WithCallerSessionID) before dispatching into this same
// Server instance — whose static callerSessionID stays "" the whole time
// (it's the shared, long-lived instance, not a per-session subprocess).

func TestOwnsSession_SharedInstance_CtxIdentity(t *testing.T) {
	s := bl91Server(t) // callerSessionID == "" — the shared-instance case
	me := mustSaveSession(t, s, "me", "")
	child := mustSaveSession(t, s, "child", me.FullID)
	unrelated := mustSaveSession(t, s, "unrelated", "")

	ctx := federation.WithCallerSessionID(context.Background(), me.FullID)
	if !s.ownsSession(ctx, me.FullID) {
		t.Error("should own itself via ctx identity")
	}
	if !s.ownsSession(ctx, child.FullID) {
		t.Error("should own its own child via ctx identity")
	}
	if s.ownsSession(ctx, unrelated.FullID) {
		t.Error("should NOT own an unrelated session via ctx identity")
	}
	// No ctx identity at all (admin / federation peer) — unscoped.
	if !s.ownsSession(context.Background(), unrelated.FullID) {
		t.Error("with no ctx identity and no static callerSessionID, should be unscoped (admin)")
	}
}

func TestHandleKillSession_SharedInstance_DeniedForUnownedSession(t *testing.T) {
	s := bl91Server(t)
	me := mustSaveSession(t, s, "me", "")
	other := mustSaveSession(t, s, "other", "")
	ctx := federation.WithCallerSessionID(context.Background(), me.FullID)

	out, err := s.handleKillSession(ctx, call(map[string]any{"session_id": other.FullID}))
	res := resultText(t, out, err)
	if !strings.Contains(res, "not found") {
		t.Errorf("expected a not-found-style denial for an unowned session, got: %q", res)
	}
	if got, _ := s.manager.GetSession(other.FullID); got.State == session.StateKilled {
		t.Error("unowned session must not actually be killed via the shared-instance ctx path")
	}
}

func TestMemoryForgetPinExport_BlockedForSharedInstanceCtxIdentity(t *testing.T) {
	s := bl91Server(t) // callerSessionID == "" — only ctx carries identity
	ctx := federation.WithCallerSessionID(context.Background(), "testhost-me")

	out, err := s.handleMemoryForget(ctx, call(map[string]any{"id": 1}))
	if res := resultText(t, out, err); !strings.Contains(res, "not available for a scoped session credential") {
		t.Errorf("memory_forget should be blocked for a ctx-scoped caller, got: %q", res)
	}
}
