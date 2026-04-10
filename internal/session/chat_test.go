package session

import (
	"testing"
)

func TestEmitChatMessage_NoCallback(t *testing.T) {
	m := &Manager{}
	// Should not panic when no callback is set
	m.EmitChatMessage("sess-123", "user", "hello", false)
}

func TestEmitChatMessage_WithCallback(t *testing.T) {
	m := &Manager{}
	var got struct {
		sessionID string
		role      string
		content   string
		streaming bool
	}
	m.SetOnChatMessage(func(sid, role, content string, streaming bool) {
		got.sessionID = sid
		got.role = role
		got.content = content
		got.streaming = streaming
	})

	m.EmitChatMessage("sess-abc", "assistant", "hello world", true)

	if got.sessionID != "sess-abc" {
		t.Errorf("sessionID = %q, want sess-abc", got.sessionID)
	}
	if got.role != "assistant" {
		t.Errorf("role = %q, want assistant", got.role)
	}
	if got.content != "hello world" {
		t.Errorf("content = %q, want hello world", got.content)
	}
	if !got.streaming {
		t.Error("streaming = false, want true")
	}
}

func TestSetOnChatMessage_Replaces(t *testing.T) {
	m := &Manager{}
	callCount := 0
	m.SetOnChatMessage(func(_, _, _ string, _ bool) {
		callCount++
	})
	m.SetOnChatMessage(func(_, _, _ string, _ bool) {
		callCount += 10
	})
	m.EmitChatMessage("x", "user", "y", false)
	if callCount != 10 {
		t.Errorf("callCount = %d, want 10 (second callback)", callCount)
	}
}
