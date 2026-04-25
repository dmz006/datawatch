package observer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPeerRegistry_RegisterMintsAndPersists(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPeerRegistry(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	token, err := r.Register("ollama-box", "B", "0.1.0", map[string]any{"os": "linux"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}

	// Verify on the same instance.
	if _, err := r.Verify("ollama-box", token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := r.Verify("ollama-box", "wrong"); err == nil {
		t.Fatalf("verify wrong token should fail")
	}

	// Persistence — load a fresh registry from the same dir.
	r2, err := NewPeerRegistry(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, err := r2.Verify("ollama-box", token); err != nil {
		t.Fatalf("verify after reload: %v", err)
	}
	if got := len(r2.List()); got != 1 {
		t.Errorf("list after reload = %d want 1", got)
	}

	// Confirm peers.json exists with 0600.
	info, err := os.Stat(filepath.Join(dir, "observer", "peers.json"))
	if err != nil {
		t.Fatalf("stat peers.json: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v want 0600", info.Mode().Perm())
	}
}

func TestPeerRegistry_RegisterRotatesToken(t *testing.T) {
	r, err := NewPeerRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t1, err := r.Register("box", "B", "0.1.0", nil)
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	t2, err := r.Register("box", "B", "0.1.0", nil)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if t1 == t2 {
		t.Fatalf("expected different tokens after re-register")
	}
	if _, err := r.Verify("box", t1); err == nil {
		t.Fatalf("old token should no longer verify")
	}
	if _, err := r.Verify("box", t2); err != nil {
		t.Fatalf("new token should verify: %v", err)
	}
}

func TestPeerRegistry_ListRedactsTokenHash(t *testing.T) {
	r, err := NewPeerRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := r.Register("a", "B", "v", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, p := range r.List() {
		if p.TokenHash != "" {
			t.Errorf("List leaked TokenHash: %s", p.TokenHash)
		}
	}
	got, ok := r.Get("a")
	if !ok || got.TokenHash != "" {
		t.Errorf("Get leaked TokenHash or missed: %+v ok=%v", got, ok)
	}
}

func TestPeerRegistry_RecordPushAndLastPayload(t *testing.T) {
	r, err := NewPeerRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := r.Register("box", "B", "v", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	snap := &StatsResponse{V: 2}
	if err := r.RecordPush("box", snap); err != nil {
		t.Fatalf("record: %v", err)
	}
	got := r.LastPayload("box")
	if got == nil {
		t.Fatal("LastPayload returned nil")
	}
	if got.V != 2 {
		t.Errorf("V = %d want 2", got.V)
	}
	entry, _ := r.Get("box")
	if entry.LastPushAt.IsZero() {
		t.Error("LastPushAt not bumped")
	}
}

func TestPeerRegistry_RecordPushUnknownPeer(t *testing.T) {
	r, err := NewPeerRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := r.RecordPush("nope", &StatsResponse{}); err == nil {
		t.Fatalf("expected error pushing to unknown peer")
	}
}

func TestPeerRegistry_DeleteRemovesAndPersists(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPeerRegistry(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := r.Register("box", "B", "v", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Delete("box"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := r.Get("box"); ok {
		t.Errorf("peer still present after delete")
	}
	r2, err := NewPeerRegistry(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(r2.List()); got != 0 {
		t.Errorf("after-delete reload = %d want 0", got)
	}
	if err := r.Delete("box"); err == nil {
		t.Errorf("second delete should error")
	}
}

func TestPeerRegistry_PersistDoesNotIncludeLastPayload(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPeerRegistry(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := r.Register("box", "B", "v", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	_ = r.RecordPush("box", &StatsResponse{V: 2})
	data, err := os.ReadFile(filepath.Join(dir, "observer", "peers.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(data); contains(got, `"v": 2`) {
		t.Errorf("LastPayload leaked into peers.json: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPeerRegistry_NewWithCorruptFileFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "observer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "observer", "peers.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewPeerRegistry(dir); err == nil {
		t.Fatalf("expected error on corrupt peers.json")
	}
}
