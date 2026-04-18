// Package auth — F10 sprint 5 (S5.1) credential broker.
//
// TokenBroker wraps a git.Provider with persistent tracking + audit
// + sweep so the parent can mint short-lived tokens for spawned
// workers without leaking long-lived secrets:
//
//   parent.MintForWorker(workerID, repo, ttl)
//     → broker.MintForWorker
//       → provider.MintToken (gh / glab)
//       → store{workerID,repo,token,expiresAt} on disk
//       → audit
//   …worker uses the token to clone + push…
//   parent.RevokeForWorker(workerID)
//     → broker.RevokeForWorker
//       → provider.RevokeToken (no-op for v1 PAT-passthrough)
//       → mark store entry revoked + audit
//
// SweepOrphans is a periodic safety net: any token whose worker no
// longer exists (or whose expiresAt has passed) is force-revoked +
// removed from the store. Run from a goroutine on a 5-min cadence;
// it's safe to call from multiple goroutines (Store has its own
// mutex).
//
// Persistence: tokens are stored in JSON at the supplied path and
// reloaded on broker construction so a daemon restart doesn't leak
// active tokens (next sweep notices + revokes them).

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// TokenRecord captures one minted token's metadata. The Token itself
// is included so SweepOrphans can call provider.RevokeToken — the
// store file should sit on a 0600 path inside the daemon's DataDir.
type TokenRecord struct {
	WorkerID  string    `json:"worker_id"`
	Repo      string    `json:"repo"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	IssuedAt  time.Time `json:"issued_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"` // zero = active
}

// Active reports whether the token is still considered usable —
// not revoked + not past expiry.
func (r *TokenRecord) Active(now time.Time) bool {
	return r.RevokedAt.IsZero() && now.Before(r.ExpiresAt)
}

// TokenStore is the persistent map of active tokens keyed by
// WorkerID. Each WorkerID can hold at most one active token at a
// time — minting a new one supersedes the old (and triggers a
// best-effort revoke).
type TokenStore struct {
	mu      sync.Mutex
	path    string
	tokens  map[string]*TokenRecord // key: WorkerID
}

// NewTokenStore loads the store at path (or creates an empty one).
// The file is JSON-encoded for operator inspection; lock down with
// 0600 perms.
func NewTokenStore(path string) (*TokenStore, error) {
	s := &TokenStore{path: path, tokens: map[string]*TokenRecord{}}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("token store dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read token store: %w", err)
	}
	if len(data) > 0 {
		var list []*TokenRecord
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("parse token store: %w", err)
		}
		for _, r := range list {
			s.tokens[r.WorkerID] = r
		}
	}
	return s, nil
}

// Get returns a snapshot of the record for workerID (nil when absent).
func (s *TokenStore) Get(workerID string) *TokenRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.tokens[workerID]
	if !ok {
		return nil
	}
	cp := *r
	return &cp
}

// List returns snapshots of every record, sorted by IssuedAt asc.
func (s *TokenStore) List() []*TokenRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*TokenRecord, 0, len(s.tokens))
	for _, r := range s.tokens {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IssuedAt.Before(out[j].IssuedAt) })
	return out
}

// put stores a record (overwrites any prior for the same WorkerID)
// and persists the file. Caller must hold s.mu.
func (s *TokenStore) put(r *TokenRecord) error {
	s.tokens[r.WorkerID] = r
	return s.persistLocked()
}

// delete removes a record + persists. Caller must hold s.mu.
func (s *TokenStore) delete(workerID string) error {
	delete(s.tokens, workerID)
	return s.persistLocked()
}

// persistLocked rewrites the file atomically. Caller must hold s.mu.
func (s *TokenStore) persistLocked() error {
	list := make([]*TokenRecord, 0, len(s.tokens))
	for _, r := range s.tokens {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].WorkerID < list[j].WorkerID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// TokenBroker is the runtime that combines a Provider, a Store, and
// an audit log. Construct once at daemon startup; safe to share
// across goroutines.
type TokenBroker struct {
	Provider git.Provider
	Store    *TokenStore

	// Audit is appended to on every mint/revoke/sweep event. Format
	// is one JSON object per line for easy `jq` inspection. nil =
	// no audit (tests use io.Discard).
	Audit io.Writer

	// MaxTTL caps the requested TTL — operators can request a 24h
	// token but get whatever min(requested, MaxTTL) gives. Defaults
	// to 1h when zero.
	MaxTTL time.Duration

	// Now overrides time.Now for testing. Defaults to time.Now.
	Now func() time.Time
}

// MintForWorker issues a token for workerID scoped to repo, valid
// for min(ttl, b.MaxTTL). If the worker already has an active
// token, it's revoked first (best-effort) before issuing a new one.
func (b *TokenBroker) MintForWorker(ctx context.Context, workerID, repo string, ttl time.Duration) (*TokenRecord, error) {
	if b.Provider == nil || b.Store == nil {
		return nil, errors.New("token broker: provider + store required")
	}
	if workerID == "" {
		return nil, errors.New("token broker: workerID required")
	}
	maxTTL := b.MaxTTL
	if maxTTL == 0 {
		maxTTL = time.Hour
	}
	if ttl == 0 || ttl > maxTTL {
		ttl = maxTTL
	}

	// Best-effort revoke of any existing token for this worker so
	// the store invariant ("at most one active token per worker") holds.
	if prior := b.Store.Get(workerID); prior != nil && prior.RevokedAt.IsZero() {
		_ = b.Provider.RevokeToken(ctx, prior.Token)
		b.audit("revoke", workerID, prior.Repo, "supersede")
	}

	minted, err := b.Provider.MintToken(ctx, repo, ttl)
	if err != nil {
		b.audit("mint-fail", workerID, repo, err.Error())
		return nil, fmt.Errorf("mint via %s: %w", b.Provider.Kind(), err)
	}

	now := b.now()
	rec := &TokenRecord{
		WorkerID:  workerID,
		Repo:      repo,
		Token:     minted.Token,
		ExpiresAt: minted.ExpiresAt,
		IssuedAt:  now,
	}
	b.Store.mu.Lock()
	err = b.Store.put(rec)
	b.Store.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("persist token record: %w", err)
	}
	b.audit("mint", workerID, repo, "")
	cp := *rec
	return &cp, nil
}

// RevokeForWorker invalidates the worker's current token. Idempotent
// — calling twice or for an unknown worker returns nil.
func (b *TokenBroker) RevokeForWorker(ctx context.Context, workerID string) error {
	if b.Store == nil {
		return errors.New("token broker: store required")
	}
	rec := b.Store.Get(workerID)
	if rec == nil || !rec.RevokedAt.IsZero() {
		return nil
	}
	if b.Provider != nil {
		_ = b.Provider.RevokeToken(ctx, rec.Token)
	}
	rec.RevokedAt = b.now()
	b.Store.mu.Lock()
	err := b.Store.put(rec)
	b.Store.mu.Unlock()
	if err != nil {
		return err
	}
	b.audit("revoke", workerID, rec.Repo, "explicit")
	return nil
}

// SweepOrphans revokes + removes tokens whose worker is no longer
// active OR whose expiresAt has passed. activeIDs is the parent's
// current snapshot of live worker IDs (typically from
// agents.Manager.List()).
//
// Returns the number of records swept. Errors from individual
// provider.Revoke calls are swallowed (logged via audit) since the
// store mutation is the source of truth — a token marked deleted
// here MUST NOT come back even if the provider call fails.
func (b *TokenBroker) SweepOrphans(ctx context.Context, activeIDs []string) (int, error) {
	if b.Store == nil {
		return 0, errors.New("token broker: store required")
	}
	active := make(map[string]struct{}, len(activeIDs))
	for _, id := range activeIDs {
		active[id] = struct{}{}
	}
	now := b.now()
	swept := 0
	for _, rec := range b.Store.List() {
		_, alive := active[rec.WorkerID]
		expired := !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt)
		if alive && !expired {
			continue
		}
		if b.Provider != nil && rec.RevokedAt.IsZero() {
			_ = b.Provider.RevokeToken(ctx, rec.Token)
		}
		b.Store.mu.Lock()
		err := b.Store.delete(rec.WorkerID)
		b.Store.mu.Unlock()
		if err != nil {
			b.audit("sweep-error", rec.WorkerID, rec.Repo, err.Error())
			continue
		}
		reason := "expired"
		if !alive {
			reason = "orphaned"
		}
		b.audit("sweep", rec.WorkerID, rec.Repo, reason)
		swept++
	}
	return swept, nil
}

// audit writes one JSON line to the audit sink. Failures here are
// non-fatal — callers must not block on audit IO.
func (b *TokenBroker) audit(event, workerID, repo, note string) {
	if b.Audit == nil {
		return
	}
	rec := struct {
		At       string `json:"at"`
		Event    string `json:"event"`
		WorkerID string `json:"worker_id"`
		Repo     string `json:"repo,omitempty"`
		Provider string `json:"provider,omitempty"`
		Note     string `json:"note,omitempty"`
	}{
		At:       b.now().UTC().Format(time.RFC3339Nano),
		Event:    event,
		WorkerID: workerID,
		Repo:     repo,
		Note:     note,
	}
	if b.Provider != nil {
		rec.Provider = b.Provider.Kind()
	}
	line, _ := json.Marshal(rec)
	_, _ = b.Audit.Write(append(line, '\n'))
}

func (b *TokenBroker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
