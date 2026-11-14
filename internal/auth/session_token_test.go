package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock is a test-injectable clock for the store's now field.
type fakeClock struct {
	t time.Time
}

func (f *fakeClock) now() time.Time { return f.t }

func newScopedStore(clock *fakeClock) (*SessionTokenStore, error) {
	s, err := NewSessionTokenStore("")
	if err != nil {
		return nil, err
	}
	if clock != nil {
		s.now = clock.now
	}
	return s, nil
}

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

func TestSessionTokenStore_MintScopedExpiryBoundary(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	s, err := newScopedStore(clock)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tok, err := s.MintScoped("sess-1", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}

	// Valid up to (but not including) the expiry instant.
	clock.t = t0.Add(time.Hour - time.Second)
	if caps, ok := s.CapsForToken(tok); !ok || len(caps) != 1 {
		t.Fatalf("expected the token to still resolve one second before expiry, ok=%v", ok)
	}
	if got := s.SessionIDForToken(tok); got != "sess-1" {
		t.Errorf("SessionIDForToken before expiry = %q, want sess-1", got)
	}

	// At and after the expiry instant the token is unknown.
	for _, at := range []time.Time{t0.Add(time.Hour), t0.Add(time.Hour).Add(time.Minute)} {
		clock.t = at
		if _, ok := s.CapsForToken(tok); ok {
			t.Errorf("at %s the token is at/past expiry and must not resolve", at)
		}
		if got := s.SessionIDForToken(tok); got != "" {
			t.Errorf("at %s SessionIDForToken = %q, want empty", at, got)
		}
	}
}

func TestSessionTokenStore_MintScopedNonPositiveTTLIsAnError(t *testing.T) {
	s, err := NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for _, ttl := range []time.Duration{0, -time.Minute} {
		if _, err := s.MintScoped("sess-1", []string{"sessions:read"}, ttl); err == nil {
			t.Errorf("MintScoped with ttl %s must error (a scoped token must expire)", ttl)
		}
	}
}

func TestSessionTokenStore_MintScopedSupersedesPrior(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	s, err := newScopedStore(clock)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tok1, err := s.MintScoped("instance:default", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}
	tok2, err := s.MintScoped("instance:default", []string{"queue:write"}, time.Minute)
	if err != nil {
		t.Fatalf("remint scoped: %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("expected a re-mint to produce a different token")
	}
	if _, ok := s.CapsForToken(tok1); ok {
		t.Error("the superseded scoped token must no longer resolve")
	}
	caps, ok := s.CapsForToken(tok2)
	if !ok || len(caps) != 1 || caps[0] != "queue:write" {
		t.Errorf("expected only the new scoped token's caps to resolve, got %v ok=%v", caps, ok)
	}

	// A plain Mint also supersedes a scoped token for the same owner.
	tok3, err := s.Mint("instance:default", []string{"memory:read"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, ok := s.CapsForToken(tok2); ok {
		t.Error("a Mint for the same owner must supersede the prior scoped token")
	}
	if _, ok := s.CapsForToken(tok3); !ok {
		t.Error("expected the Mint token to resolve")
	}
}

func TestSessionTokenStore_ExpiresAtRoundTripsThroughReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_tokens.json")

	s1, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	// A real-clock-relative expiry in the future so the default real
	// clock of the reloaded store still sees the token as unexpired.
	tok, err := s1.MintScoped("instance:deploy", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}

	// The on-disk record carries expires_at.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	var list []*SessionTokenRecord
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("parse store file: %v", err)
	}
	if len(list) != 1 || list[0].Token != tok || list[0].ExpiresAt.IsZero() {
		t.Fatalf("expected one on-disk record with a set expires_at, got %+v", list)
	}
	if list[0].ExpiresAt.Sub(time.Now()) > time.Hour || list[0].ExpiresAt.Before(time.Now()) {
		t.Errorf("persisted ExpiresAt %v not ~1h in the future (now %v)", list[0].ExpiresAt, time.Now())
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected the store file to be 0600, got %o", info.Mode().Perm())
	}

	s2, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := s2.CapsForToken(tok); !ok {
		t.Fatal("expected a scoped token to still resolve after reload (unexpired)")
	}
	if got := s2.SessionIDForToken(tok); got != "instance:deploy" {
		t.Errorf("SessionIDForToken after reload = %q, want instance:deploy", got)
	}

	// Once the reloaded store's clock passes the expiry, it stops
	// resolving again — the expiry is state, not just in-memory luck.
	s2.now = func() time.Time { return list[0].ExpiresAt.Add(time.Second) }
	if _, ok := s2.CapsForToken(tok); ok {
		t.Error("expected the token to be unknown once its persisted expiry has passed")
	}
}

// TestSessionTokenStore_LegacyFileWithoutExpiresAtStaysValid pins the
// backwards-compat guarantee: a store file written before expiring
// tokens existed (no expires_at field at all) loads, and its tokens
// remain valid — a zero ExpiresAt means no expiry.
func TestSessionTokenStore_LegacyFileWithoutExpiresAtStaysValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_tokens.json")
	legacy := `[
  {
    "session_id": "sess-legacy",
    "token": "legacytoken0123456789abcdef",
    "caps": [
      "sessions:read"
    ]
  }
]`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	s, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	if caps, ok := s.CapsForToken("legacytoken0123456789abcdef"); !ok || len(caps) != 1 || caps[0] != "sessions:read" {
		t.Fatalf("expected the legacy token to resolve with its caps, got %v ok=%v", caps, ok)
	}
	if got := s.SessionIDForToken("legacytoken0123456789abcdef"); got != "sess-legacy" {
		t.Errorf("SessionIDForToken = %q, want sess-legacy", got)
	}

	// A legacy record whose session is still active also survives a sweep.
	swept, err := s.SweepOrphans([]string{"sess-legacy"})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 0 {
		t.Errorf("expected 0 swept for an active session, got %d", swept)
	}
	if _, ok := s.CapsForToken("legacytoken0123456789abcdef"); !ok {
		t.Error("the legacy token must survive a sweep while its session is active")
	}
}

func TestSessionTokenStore_SweepKeepsUnexpiredReservedPrefix(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	s, err := newScopedStore(clock)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	instanceTok, err := s.MintScoped("instance:default", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}
	orphanTok, _ := s.Mint("sess-dead", []string{"sessions:read"})

	// No sessions are active: the plain orphan goes, the reserved
	// prefix record stays.
	swept, err := s.SweepOrphans(nil)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 1 {
		t.Errorf("expected 1 swept (the plain orphan only), got %d", swept)
	}
	if _, ok := s.CapsForToken(instanceTok); !ok {
		t.Error("an unexpired instance: record must survive a sweep even though it is not in the active session list")
	}
	if _, ok := s.CapsForToken(orphanTok); ok {
		t.Error("a plain orphan record must be removed by the sweep")
	}

	// Once expired, even the reserved prefix record is swept.
	clock.t = t0.Add(time.Hour + time.Second)
	swept, err = s.SweepOrphans(nil)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 1 {
		t.Errorf("expected the expired instance: record to be swept, got %d", swept)
	}
	if _, ok := s.CapsForToken(instanceTok); ok {
		t.Error("an expired instance: record must not survive a sweep")
	}
}

func TestSessionTokenStore_SweepRemovesExpiredOfAnyOwner(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	s, err := newScopedStore(clock)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	longTok, err := s.MintScoped("instance:long", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}
	shortTok, err := s.MintScoped("instance:short", []string{"queue:write"}, time.Minute)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}

	// Both unexpired: nothing to sweep.
	swept, err := s.SweepOrphans(nil)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 0 {
		t.Fatalf("expected 0 swept before any expiry, got %d", swept)
	}

	// One lapses, the other is still valid.
	clock.t = t0.Add(30 * time.Minute)
	swept, err = s.SweepOrphans(nil)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if swept != 1 {
		t.Errorf("expected only the lapsed record swept, got %d", swept)
	}
	if _, ok := s.CapsForToken(longTok); !ok {
		t.Error("an unexpired record must survive the sweep")
	}
	if _, ok := s.CapsForToken(shortTok); ok {
		t.Error("a lapsed record must be swept even with the reserved prefix")
	}
}

func TestSessionTokenStore_RevokeScopedOwner(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	s, err := newScopedStore(clock)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tok, err := s.MintScoped("instance:default", []string{"sessions:read"}, time.Hour)
	if err != nil {
		t.Fatalf("mint scoped: %v", err)
	}
	if _, ok := s.CapsForToken(tok); !ok {
		t.Fatal("expected the scoped token to resolve before revoke")
	}
	if err := s.Revoke("instance:default"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := s.CapsForToken(tok); ok {
		t.Error("a revoked scoped token must not resolve")
	}
	// Idempotent, exactly as for session ids.
	if err := s.Revoke("instance:default"); err != nil {
		t.Errorf("a second revoke of the same owner must be a no-op, got %v", err)
	}
}

func TestSessionTokenStore_MintScopedPersists0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_tokens.json")
	s, err := NewSessionTokenStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := s.MintScoped("instance:default", []string{"sessions:read"}, time.Hour); err != nil {
		t.Fatalf("mint scoped: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected the store file to exist on disk: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected the store file to be 0600, got %o", info.Mode().Perm())
	}
}
