package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionTokenStore_MintResolveRevoke(t *testing.T) {
	s, err := NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tok, err := s.Mint("sess-1", []string{"sessions:read", "memory:read"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(tok) != 64 { // 32 bytes hex-encoded
		t.Errorf("expected a 64-char hex token, got %d chars: %q", len(tok), tok)
	}
	caps, ok := s.CapsForToken(tok)
	if !ok {
		t.Fatal("expected the minted token to resolve")
	}
	if len(caps) != 2 || caps[0] != "sessions:read" || caps[1] != "memory:read" {
		t.Errorf("unexpected caps: %v", caps)
	}
	if got := s.SessionIDForToken(tok); got != "sess-1" {
		t.Errorf("SessionIDForToken = %q, want sess-1", got)
	}

	if err := s.Revoke("sess-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := s.CapsForToken(tok); ok {
		t.Error("expected the revoked token to no longer resolve")
	}
}

func TestSessionTokenStore_MintSupersedesPrior(t *testing.T) {
	s, _ := NewSessionTokenStore("")
	tok1, _ := s.Mint("sess-1", []string{"sessions:read"})
	tok2, _ := s.Mint("sess-1", []string{"sessions:write"})
	if tok1 == tok2 {
		t.Fatal("expected a new mint to produce a different token")
	}
	if _, ok := s.CapsForToken(tok1); ok {
		t.Error("the superseded token must no longer resolve")
	}
	caps, ok := s.CapsForToken(tok2)
	if !ok || len(caps) != 1 || caps[0] != "sessions:write" {
		t.Errorf("expected only the new token's caps to resolve, got %v ok=%v", caps, ok)
	}
}

func TestSessionTokenStore_UnknownTokenRejected(t *testing.T) {
	s, _ := NewSessionTokenStore("")
	if _, ok := s.CapsForToken("not-a-real-token"); ok {
		t.Error("an unknown token must not resolve")
	}
}

func TestSessionTokenStore_RevokeUnknownSessionIsNoop(t *testing.T) {
	s, _ := NewSessionTokenStore("")
	if err := s.Revoke("never-existed"); err != nil {
		t.Errorf("revoking an unknown session should be a no-op, got error: %v", err)
	}
}

func TestSessionTokenStore_SweepOrphans(t *testing.T) {
	s, _ := NewSessionTokenStore("")
	tokAlive, _ := s.Mint("sess-alive", []string{"sessions:read"})
	tokDead, _ := s.Mint("sess-dead", []string{"sessions:read"})

	swept, err := s.SweepOrphans([]string{"sess-alive"})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 1 {
		t.Errorf("expected 1 swept, got %d", swept)
	}
	if _, ok := s.CapsForToken(tokAlive); !ok {
		t.Error("the still-active session's token must survive the sweep")
	}
	if _, ok := s.CapsForToken(tokDead); ok {
		t.Error("the orphaned session's token must be removed by the sweep")
	}
}

// TestSessionTokenStore_SurvivesReload confirms the whole point of
// persisting to disk: an already-minted token for an already-running
// session (a live orphan whose bridge process never restarted) must
// still resolve after the store is reloaded from a fresh process,
// exactly as it would after a daemon restart.
func TestSessionTokenStore_SurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_tokens.json")

	s1, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tok, err := s1.Mint("sess-1", []string{"sessions:read", "queue:write"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected the store file to exist on disk: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected the store file to be 0600, got %o", info.Mode().Perm())
	}

	s2, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	caps, ok := s2.CapsForToken(tok)
	if !ok {
		t.Fatal("expected the token minted before reload to still resolve after reload")
	}
	if len(caps) != 2 || caps[0] != "sessions:read" || caps[1] != "queue:write" {
		t.Errorf("unexpected caps after reload: %v", caps)
	}
}

func TestSessionTokenStore_RevokePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_tokens.json")

	s1, _ := NewSessionTokenStore(path)
	tok, _ := s1.Mint("sess-1", []string{"sessions:read"})
	if err := s1.Revoke("sess-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	s2, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := s2.CapsForToken(tok); ok {
		t.Error("a revoked token must not resolve after reload")
	}
}
