package autonomous

// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run SessionBackend

import "testing"

func TestSessionBackendHookRoundTrip(t *testing.T) {
	m := &Manager{}
	if m.sessionBackend != nil {
		t.Fatal("hook should start unset")
	}
	m.SetSessionBackend(func(id string) (string, string) {
		if id == "s1" {
			return "pi-prompt", "qwen3.8:27b"
		}
		return "", ""
	})
	m.mu.Lock()
	fn := m.sessionBackend
	m.mu.Unlock()
	if fn == nil {
		t.Fatal("hook not stored")
	}
	if b, mod := fn("s1"); b != "pi-prompt" || mod != "qwen3.8:27b" {
		t.Fatalf("got %q %q", b, mod)
	}
	if b, mod := fn("nope"); b != "" || mod != "" {
		t.Fatalf("unknown session returned %q %q", b, mod)
	}
	m.SetSessionBackend(nil)
	if m.sessionBackend != nil {
		t.Fatal("nil should clear the hook")
	}
}
