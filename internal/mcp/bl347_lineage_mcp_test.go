// BL347 — Session lineage: MCP tool handler tests for session_children and reply_to_parent.

package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// ── session_children ──────────────────────────────────────────────────────────

func TestSessionChildren_NoParentID(t *testing.T) {
	s := bl91Server(t)
	res := invoke(t, s.handleSessionChildren, map[string]any{})
	// BL354: message updated to mention session_name as an alternative.
	if !strings.Contains(res, "session_id or session_name is required") {
		t.Errorf("want required error, got %q", res)
	}
}

func TestSessionChildren_ParentNotFound(t *testing.T) {
	s := bl91Server(t)
	res := invoke(t, s.handleSessionChildren, map[string]any{"session_id": "zzz99"})
	if !strings.Contains(res, "not found") {
		t.Errorf("want not-found, got %q", res)
	}
}

func TestSessionChildren_NoChildren(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "pp01", FullID: "testhost-pp01", Hostname: "testhost",
		Task: "parent task", State: session.StateRunning, UpdatedAt: time.Now(),
	})
	res := invoke(t, s.handleSessionChildren, map[string]any{"session_id": "pp01"})
	if !strings.Contains(res, "no child sessions") {
		t.Errorf("want no-child message, got %q", res)
	}
}

func TestSessionChildren_ListsChildren(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "pp02", FullID: "testhost-pp02", Hostname: "testhost",
		Task: "parent task", State: session.StateRunning,
		UpdatedAt: time.Now(), CreatedAt: time.Now(),
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cc02", FullID: "testhost-cc02", Hostname: "testhost",
		ParentID: "testhost-pp02",
		Task:     "child task A", State: session.StateRunning,
		UpdatedAt: time.Now(), CreatedAt: time.Now().Add(time.Second),
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cc03", FullID: "testhost-cc03", Hostname: "testhost",
		ParentID: "testhost-pp02",
		Task:     "child task B", State: session.StateComplete,
		UpdatedAt: time.Now(), CreatedAt: time.Now().Add(2 * time.Second),
	})

	res := invoke(t, s.handleSessionChildren, map[string]any{"session_id": "pp02"})
	if !strings.Contains(res, "Children of session pp02") {
		t.Errorf("want header, got %q", res)
	}
	if !strings.Contains(res, "cc02") || !strings.Contains(res, "cc03") {
		t.Errorf("want both children, got %q", res)
	}
}

// ── reply_to_parent ───────────────────────────────────────────────────────────

func TestReplyToParent_NoText(t *testing.T) {
	s := bl91Server(t)
	res := invoke(t, s.handleReplyToParent, map[string]any{"session_id": "pp01"})
	if !strings.Contains(res, "text is required") {
		t.Errorf("want text-required error, got %q", res)
	}
}

func TestReplyToParent_NoParentRecorded(t *testing.T) {
	s := bl91Server(t)
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cc10", FullID: "testhost-cc10", Hostname: "testhost",
		Task: "orphan child", State: session.StateRunning, UpdatedAt: time.Now(),
	})
	res := invoke(t, s.handleReplyToParent, map[string]any{
		"session_id": "cc10",
		"text":       "hello parent",
	})
	if !strings.Contains(res, "No parent session found") {
		t.Errorf("want no-parent message, got %q", res)
	}
}

func TestReplyToParent_ParentGone(t *testing.T) {
	s := bl91Server(t)
	// Child references a parent that doesn't exist in the store.
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cc11", FullID: "testhost-cc11", Hostname: "testhost",
		ParentID: "testhost-gone99",
		Task:     "child of ghost", State: session.StateRunning, UpdatedAt: time.Now(),
	})
	res := invoke(t, s.handleReplyToParent, map[string]any{
		"session_id": "cc11",
		"text":       "are you there?",
	})
	if !strings.Contains(res, "no longer exists") {
		t.Errorf("want gone message, got %q", res)
	}
}

func TestReplyToParent_Success(t *testing.T) {
	s := bl91Server(t)
	// Parent must be in a state that accepts SendInput.
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "pp20", FullID: "testhost-pp20", Hostname: "testhost",
		TmuxSession: "cs-pp20",
		Task:        "parent task", State: session.StateWaitingInput,
		UpdatedAt: time.Now(), CreatedAt: time.Now(),
	})
	s.manager.SaveSession(&session.Session{ //nolint:errcheck
		ID: "cc20", FullID: "testhost-cc20", Hostname: "testhost",
		ParentID: "testhost-pp20",
		Task:     "child task", State: session.StateRunning,
		UpdatedAt: time.Now(), CreatedAt: time.Now().Add(time.Second),
	})

	res := invoke(t, s.handleReplyToParent, map[string]any{
		"session_id": "cc20",
		"text":       "done with my part",
	})
	if !strings.Contains(res, "Sent to parent session pp20") {
		t.Errorf("want success message, got %q", res)
	}
}
