// Design A3 (docs/plans/historical-plans/2026-09-02-sec-design-a-authz-scoping.md
// §A3) — per-session scoped credential. A spawned session's bridge/MCP
// connection used to hold the real admin token (HLLM-001/002's root
// cause: "session = full-admin principal"). SessionTokenStore mints a
// distinct, revocable, capability-scoped token per session instead.
//
// Persistence: unlike nonce.go's short-lived nonces, a session token
// must survive a daemon restart — a live orphan session's bridge
// process keeps running across a restart (BL93) with its token already
// baked into its environment, and has no way to learn a new one short
// of being relaunched. The store is persisted to a 0600 JSON file under
// DataDir and reloaded at construction so an already-minted token for
// an already-running session stays valid.
//
// Expiry: a record may optionally carry ExpiresAt. Zero means no expiry
// (as for tokens minted before expiring tokens existed, or minted via
// Mint, which still produces non-expiring credentials). MintScoped sets
// ExpiresAt to now+ttl, so the daemon can hand a launched tool a
// short-lived credential that stops resolving once it lapses. A token
// whose ExpiresAt is set and not after the current time is treated as
// unknown by CapsForToken/SessionIDForToken, and SweepOrphans removes
// every expired record regardless of owner.
//
// Reserved owner prefix: Mint/Revoke/Sweep are keyed on the record's
// owner id — a session id for ordinary tokens, but any owner id for
// scoped ones. InstanceTokenOwnerPrefix ("instance:") is reserved for
// credentials not tied to one session (e.g. a tool handed an
// instance-scoped short-lived token); SweepOrphans keeps an unexpired
// record whose owner id carries that prefix even though it is not in
// the active session list.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// InstanceTokenOwnerPrefix is the reserved owner-id prefix for scoped
// credentials that are not tied to one session (a tool credential minted
// for the instance as a whole, say, not for a specific session).
// SweepOrphans keeps an unexpired record whose SessionID (owner id) has
// this prefix even though it cannot appear in the active session list.
const InstanceTokenOwnerPrefix = "instance:"

// SessionTokenRecord is one session's current scoped credential.
type SessionTokenRecord struct {
	SessionID string   `json:"session_id"`
	Token     string   `json:"token"`
	Caps      []string `json:"caps"`
	// ExpiresAt, when set, is the time after this token no longer
	// resolves. Zero means no expiry (the default, and the only value
	// produced by Mint and by store files written before expiring
	// tokens existed). JSON-omitted when zero so legacy files keep their
	// exact shape.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// SessionTokenStore holds, at most, one active token per session.
// Minting a new token for a session supersedes (invalidates) its prior
// one. Safe for concurrent use.
type SessionTokenStore struct {
	mu        sync.RWMutex
	path      string
	byToken   map[string]*SessionTokenRecord // token -> record
	bySession map[string]string              // sessionID -> token
	now       func() time.Time               // injectable clock; time.Now by default
}

// NewSessionTokenStore loads the store at path (creating an empty one
// if it doesn't exist yet). path should sit under the daemon's DataDir.
func NewSessionTokenStore(path string) (*SessionTokenStore, error) {
	s := &SessionTokenStore{
		path:      path,
		byToken:   map[string]*SessionTokenRecord{},
		bySession: map[string]string{},
		now:       time.Now,
	}
	if path == "" {
		return s, nil // in-memory only (tests)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("session token store dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read session token store: %w", err)
	}
	if len(data) > 0 {
		var list []*SessionTokenRecord
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("parse session token store: %w", err)
		}
		for _, r := range list {
			s.byToken[r.Token] = r
			s.bySession[r.SessionID] = r.Token
		}
	}
	return s, nil
}

// Mint issues a new 256-bit token for sessionID scoped to caps,
// superseding (and removing) any prior token for that session.
func (s *SessionTokenStore) Mint(sessionID string, caps []string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint session token: %w", err)
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.bySession[sessionID]; ok {
		delete(s.byToken, old)
	}
	rec := &SessionTokenRecord{SessionID: sessionID, Token: tok, Caps: caps}
	s.byToken[tok] = rec
	s.bySession[sessionID] = tok
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	return tok, nil
}

// MintScoped issues a new 256-bit token for ownerID scoped to caps
// that expires after ttl, superseding (and removing) any prior token
// for that owner id exactly as Mint does. A scoped token must expire:
// a ttl <= 0 is an error. ownerID may be a session id or a reserved
// owner id such as InstanceTokenOwnerPrefix for a credential that is
// not tied to one session.
func (s *SessionTokenStore) MintScoped(ownerID string, caps []string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", fmt.Errorf("mint scoped token: ttl must be positive, got %s", ttl)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint scoped token: %w", err)
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.bySession[ownerID]; ok {
		delete(s.byToken, old)
	}
	rec := &SessionTokenRecord{SessionID: ownerID, Token: tok, Caps: caps, ExpiresAt: s.now().Add(ttl)}
	s.byToken[tok] = rec
	s.bySession[ownerID] = tok
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	return tok, nil
}

// SetNowForTest overrides the store's clock — the now function backing
// MintScoped's ExpiresAt stamp and every expiry check — for
// cross-package tests that must fast-forward time deterministically
// without sleeping (e.g. internal/server's instance-token tests). Same
// package tests set the now field directly instead. Production code
// never calls this; nil restores time.Now.
func (s *SessionTokenStore) SetNowForTest(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// expired reports whether the record carries an expiry that is
// (inclusively) already due. Callers need not hold s.mu; the record
// pointer comes from the store's own maps.
func (s *SessionTokenStore) expired(r *SessionTokenRecord) bool {
	return !r.ExpiresAt.IsZero() && !r.ExpiresAt.After(s.now())
}

// CapsForToken resolves a bearer token to its session's capability
// list. ok is false for an unknown, revoked, or expired token.
func (s *SessionTokenStore) CapsForToken(tok string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byToken[tok]
	if !ok || s.expired(r) {
		return nil, false
	}
	return r.Caps, true
}

// SessionIDForToken returns which session a token belongs to, or ""
// if unknown or expired. Used for audit/attribution.
func (s *SessionTokenStore) SessionIDForToken(tok string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.byToken[tok]; ok && !s.expired(r) {
		return r.SessionID
	}
	return ""
}

// ListOwners returns, sorted, the owner ids that hold a live token and start
// with prefix. It never returns a token.
func (s *SessionTokenStore) ListOwners(prefix string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for owner, tok := range s.bySession {
		if !strings.HasPrefix(owner, prefix) {
			continue
		}
		if r, ok := s.byToken[tok]; ok && !s.expired(r) {
			out = append(out, owner)
		}
	}
	sort.Strings(out)
	return out
}

// Revoke invalidates ownerID's token immediately (session kill,
// stop_all_sessions). It works for any owner id minted via Mint or
// MintScoped, including reserved prefix ids such as
// InstanceTokenOwnerPrefix. Idempotent.
func (s *SessionTokenStore) Revoke(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.bySession[sessionID]
	if !ok {
		return nil
	}
	delete(s.byToken, tok)
	delete(s.bySession, sessionID)
	return s.persistLocked()
}

// SweepOrphans removes tokens for sessions no longer present in
// activeSessionIDs (a snapshot from session.Manager.ListSessions()).
// It also removes every record whose expiry is already due, of any
// owner (including instance-owner and active-session ones); both kinds
// of removal count in the returned total. An unexpired record whose
// owner id is a reserved id with the InstanceTokenOwnerPrefix (a
// credential not tied to one session) is kept even though it cannot be
// in the active session list. Returns the number of records removed.
// Call periodically and once right after startup reconciliation
// completes.
func (s *SessionTokenStore) SweepOrphans(activeSessionIDs []string) (int, error) {
	active := make(map[string]struct{}, len(activeSessionIDs))
	for _, id := range activeSessionIDs {
		active[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	swept := 0
	for sessionID, tok := range s.bySession {
		r := s.byToken[tok]
		if s.expired(r) {
			delete(s.byToken, tok)
			delete(s.bySession, sessionID)
			swept++
			continue
		}
		if _, ok := active[sessionID]; ok {
			continue
		}
		if strings.HasPrefix(sessionID, InstanceTokenOwnerPrefix) {
			continue
		}
		delete(s.byToken, tok)
		delete(s.bySession, sessionID)
		swept++
	}
	if swept > 0 {
		if err := s.persistLocked(); err != nil {
			return swept, err
		}
	}
	return swept, nil
}

// persistLocked rewrites the store file atomically. Caller must hold s.mu.
func (s *SessionTokenStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	list := make([]*SessionTokenRecord, 0, len(s.byToken))
	for _, r := range s.byToken {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].SessionID < list[j].SessionID })
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
