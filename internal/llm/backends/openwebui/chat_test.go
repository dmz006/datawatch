package openwebui

import (
	"testing"
)

func TestSetChatEmitter(t *testing.T) {
	// Reset global state
	chatEmitter = nil
	defer func() { chatEmitter = nil }()

	var got struct {
		sessionID string
		role      string
		content   string
		streaming bool
	}
	SetChatEmitter(func(sid, role, content string, streaming bool) {
		got.sessionID = sid
		got.role = role
		got.content = content
		got.streaming = streaming
	})

	emitChat("cs-test-session", "user", "hello", false)

	if got.sessionID != "test-session" {
		t.Errorf("sessionID = %q, want 'test-session' (cs- prefix stripped)", got.sessionID)
	}
	if got.role != "user" {
		t.Errorf("role = %q, want user", got.role)
	}
	if got.content != "hello" {
		t.Errorf("content = %q, want hello", got.content)
	}
	if got.streaming {
		t.Error("streaming should be false")
	}
}

func TestEmitChat_NoEmitter(t *testing.T) {
	chatEmitter = nil
	// Should not panic
	emitChat("cs-x", "assistant", "test", true)
}

func TestEmitChat_StripsCsPrefix(t *testing.T) {
	chatEmitter = nil
	defer func() { chatEmitter = nil }()

	var gotSID string
	SetChatEmitter(func(sid, _, _ string, _ bool) { gotSID = sid })

	emitChat("cs-my-session-id", "user", "", false)
	if gotSID != "my-session-id" {
		t.Errorf("sessionID = %q, want 'my-session-id'", gotSID)
	}

	// Without cs- prefix
	emitChat("plain-id", "user", "", false)
	if gotSID != "plain-id" {
		t.Errorf("sessionID = %q, want 'plain-id' (no prefix to strip)", gotSID)
	}
}

func TestInteractiveBackend_Name(t *testing.T) {
	b := NewInteractive("http://localhost:3000", "key", "llama3")
	if b.Name() != "openwebui" {
		t.Errorf("Name() = %q, want openwebui", b.Name())
	}
	if !b.SupportsInteractiveInput() {
		t.Error("SupportsInteractiveInput should be true")
	}
}

func TestNewInteractive_Defaults(t *testing.T) {
	b := NewInteractive("", "", "").(*InteractiveBackend)
	if b.baseURL != "http://localhost:3000" {
		t.Errorf("baseURL = %q, want default", b.baseURL)
	}
	if b.model != "llama3" {
		t.Errorf("model = %q, want llama3 default", b.model)
	}
}
