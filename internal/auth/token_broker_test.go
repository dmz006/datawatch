// F10 sprint 5 (S5.1) — token broker tests.

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// fakeProvider records every call and returns canned responses.
// Tests construct one per case with the response/err they want.
type fakeProvider struct {
	mu          sync.Mutex
	mintCalls   []string // repo per call
	revokeCalls []string // token per call
	mintErr     error
	revokeErr   error
	tokenSeq    int
}

func (f *fakeProvider) Kind() string { return "fake" }
func (f *fakeProvider) MintToken(_ context.Context, repo string, ttl time.Duration) (*git.MintedToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mintCalls = append(f.mintCalls, repo)
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	f.tokenSeq++
	return &git.MintedToken{
		Token:     "fake-tok-" + repo,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}
func (f *fakeProvider) RevokeToken(_ context.Context, tok string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalls = append(f.revokeCalls, tok)
	return f.revokeErr
}
func (f *fakeProvider) OpenPR(_ context.Context, _ git.PROptions) (string, error) {
	return "", nil
}

// newBroker spins up a broker with a fresh store + provided provider.
func newBroker(t *testing.T, p git.Provider) (*TokenBroker, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewTokenStore(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	audit := &bytes.Buffer{}
	return &TokenBroker{
		Provider: p,
		Store:    store,
		Audit:    audit,
		MaxTTL:   time.Hour,
	}, audit
}

func TestTokenBroker_Mint_PersistsRecord(t *testing.T) {
	b, audit := newBroker(t, &fakeProvider{})
	rec, err := b.MintForWorker(context.Background(), "w1", "owner/repo", 30*time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if rec.Token == "" || rec.WorkerID != "w1" || rec.Repo != "owner/repo" {
		t.Errorf("rec=%+v", rec)
	}
	if !rec.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt should be in future: %v", rec.ExpiresAt)
	}
	got := b.Store.Get("w1")
	if got == nil || got.Token != rec.Token {
		t.Error("record not in store")
	}
	if !strings.Contains(audit.String(), `"event":"mint"`) {
		t.Errorf("audit missing mint event: %s", audit.String())
	}
}

func TestTokenBroker_Mint_TTLCappedByMax(t *testing.T) {
	b, _ := newBroker(t, &fakeProvider{})
	b.MaxTTL = 10 * time.Minute
	rec, err := b.MintForWorker(context.Background(), "w1", "x/y", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	delta := time.Until(rec.ExpiresAt)
	if delta > 11*time.Minute {
		t.Errorf("ttl not capped: ExpiresAt - now = %v", delta)
	}
}

func TestTokenBroker_Mint_SupersedesExisting(t *testing.T) {
	p := &fakeProvider{}
	b, _ := newBroker(t, p)
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	// First mint had no prior token to revoke; second mint should revoke the first.
	if len(p.revokeCalls) != 1 {
		t.Errorf("expected 1 revoke (supersede), got %d", len(p.revokeCalls))
	}
	if len(p.mintCalls) != 2 {
		t.Errorf("expected 2 mints, got %d", len(p.mintCalls))
	}
	if got := b.Store.Get("w1"); got == nil || got.Token != "fake-tok-x/y" {
		t.Errorf("store doesn't reflect supersede: %+v", got)
	}
}

func TestTokenBroker_Mint_ProviderErrorAudited(t *testing.T) {
	p := &fakeProvider{mintErr: errors.New("rate limited")}
	b, audit := newBroker(t, p)
	_, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(audit.String(), `"event":"mint-fail"`) {
		t.Errorf("audit missing mint-fail: %s", audit.String())
	}
	if b.Store.Get("w1") != nil {
		t.Error("store should be empty after failed mint")
	}
}

func TestTokenBroker_Mint_RequiresWorkerID(t *testing.T) {
	b, _ := newBroker(t, &fakeProvider{})
	if _, err := b.MintForWorker(context.Background(), "", "x", time.Hour); err == nil {
		t.Error("expected error for empty workerID")
	}
}

func TestTokenBroker_Revoke_MarksRecordRevoked(t *testing.T) {
	p := &fakeProvider{}
	b, audit := newBroker(t, p)
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeForWorker(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	got := b.Store.Get("w1")
	if got == nil || got.RevokedAt.IsZero() {
		t.Errorf("record not marked revoked: %+v", got)
	}
	if got.Active(time.Now()) {
		t.Error("revoked record should not be Active")
	}
	if len(p.revokeCalls) != 1 {
		t.Errorf("expected 1 revoke call, got %d", len(p.revokeCalls))
	}
	if !strings.Contains(audit.String(), `"event":"revoke"`) {
		t.Errorf("audit missing revoke: %s", audit.String())
	}
}

func TestTokenBroker_Revoke_Idempotent(t *testing.T) {
	p := &fakeProvider{}
	b, _ := newBroker(t, p)
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeForWorker(context.Background(), "w1"); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeForWorker(context.Background(), "w1"); err != nil {
		t.Errorf("second revoke should be no-op: %v", err)
	}
	if err := b.RevokeForWorker(context.Background(), "unknown"); err != nil {
		t.Errorf("revoke for unknown worker should be no-op: %v", err)
	}
	if len(p.revokeCalls) != 1 {
		t.Errorf("expected exactly 1 provider revoke, got %d", len(p.revokeCalls))
	}
}

func TestTokenBroker_Sweep_RemovesOrphansAndExpired(t *testing.T) {
	p := &fakeProvider{}
	b, audit := newBroker(t, p)
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }

	// Drop records directly into the store with explicit expiries so
	// the test isn't at the mercy of the fake provider's wall-clock
	// computation.
	b.Store.mu.Lock()
	_ = b.Store.put(&TokenRecord{
		WorkerID: "w1", Repo: "x/y", Token: "tok-w1",
		ExpiresAt: now.Add(time.Hour), IssuedAt: now,
	})
	_ = b.Store.put(&TokenRecord{
		WorkerID: "w2", Repo: "x/y", Token: "tok-w2",
		ExpiresAt: now.Add(-time.Hour), IssuedAt: now.Add(-2 * time.Hour),
	})
	_ = b.Store.put(&TokenRecord{
		WorkerID: "w3", Repo: "x/y", Token: "tok-w3",
		ExpiresAt: now.Add(time.Hour), IssuedAt: now,
	})
	b.Store.mu.Unlock()

	// Sweep with w1 + w2 reported alive. w2 still expires; w3 is
	// orphaned. This isolates the "expired" reason on w2 and
	// "orphaned" on w3 so the audit assertions below distinguish.
	swept, err := b.SweepOrphans(context.Background(), []string{"w1", "w2"})
	if err != nil {
		t.Fatal(err)
	}
	if swept != 2 {
		t.Errorf("swept=%d want 2 (w2 expired + w3 orphan)", swept)
	}
	// w1 must still be present
	if got := b.Store.Get("w1"); got == nil {
		t.Error("w1 should survive sweep")
	}
	if got := b.Store.Get("w2"); got != nil {
		t.Errorf("w2 should be removed: %+v", got)
	}
	if got := b.Store.Get("w3"); got != nil {
		t.Errorf("w3 should be removed: %+v", got)
	}
	auditStr := audit.String()
	if !strings.Contains(auditStr, `"event":"sweep"`) {
		t.Errorf("audit missing sweep events: %s", auditStr)
	}
	// Both swept records should appear with their reasons.
	if !strings.Contains(auditStr, `"note":"expired"`) {
		t.Error("audit missing expired sweep")
	}
	if !strings.Contains(auditStr, `"note":"orphaned"`) {
		t.Error("audit missing orphaned sweep")
	}
}

func TestTokenBroker_Sweep_NoActiveWorkers(t *testing.T) {
	b, _ := newBroker(t, &fakeProvider{})
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	swept, err := b.SweepOrphans(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Errorf("swept=%d want 1 (everything orphaned)", swept)
	}
}

// Persistence: a store reloaded from disk sees the same records.
func TestTokenStore_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	s1, _ := NewTokenStore(path)
	s1.mu.Lock()
	_ = s1.put(&TokenRecord{
		WorkerID: "w1", Repo: "x/y", Token: "tok",
		ExpiresAt: time.Now().Add(time.Hour), IssuedAt: time.Now(),
	})
	s1.mu.Unlock()

	s2, err := NewTokenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("w1"); got == nil || got.Token != "tok" {
		t.Errorf("reload missed w1: %+v", got)
	}
}

// Audit lines must be valid JSON one-per-line so `jq` works.
func TestTokenBroker_AuditLineIsValidJSON(t *testing.T) {
	b, audit := newBroker(t, &fakeProvider{})
	if _, err := b.MintForWorker(context.Background(), "w1", "x/y", time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("invalid JSON: %q (%v)", line, err)
		}
	}
}
