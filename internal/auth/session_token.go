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
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// SessionTokenRecord is one session's current scoped credential.
type SessionTokenRecord struct {
	SessionID string   `json:"session_id"`
	Token     string   `json:"token"`
	Caps      []string `json:"caps"`
}

// SessionTokenStore holds, at most, one active token per session.
// Minting a new token for a session supersedes (invalidates) its prior
// one. Safe for concurrent use.
type SessionTokenStore struct {
	mu        sync.RWMutex
	path      string
	byToken   map[string]*SessionTokenRecord // token -> record
	bySession map[string]string              // sessionID -> token
}

// NewSessionTokenStore loads the store at path (creating an empty one
// if it doesn't exist yet). path should sit under the daemon's DataDir.
func NewSessionTokenStore(path string) (*SessionTokenStore, error) {
	s := &SessionTokenStore{
		path:      path,
		byToken:   map[string]*SessionTokenRecord{},
		bySession: map[string]string{},
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

// CapsForToken resolves a bearer token to its session's capability
// list. ok is false for an unknown/revoked token.
func (s *SessionTokenStore) CapsForToken(tok string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byToken[tok]
	if !ok {
		return nil, false
	}
	return r.Caps, true
}

// SessionIDForToken returns which session a token belongs to, or ""
// if unknown. Used for audit/attribution.
func (s *SessionTokenStore) SessionIDForToken(tok string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.byToken[tok]; ok {
		return r.SessionID
	}
	return ""
}

// Revoke invalidates sessionID's token immediately (session kill,
// stop_all_sessions). Idempotent.
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
// Returns the number of records removed. Call periodically and once
// right after startup reconciliation completes.
func (s *SessionTokenStore) SweepOrphans(activeSessionIDs []string) (int, error) {
	active := make(map[string]struct{}, len(activeSessionIDs))
	for _, id := range activeSessionIDs {
		active[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	swept := 0
	for sessionID, tok := range s.bySession {
		if _, ok := active[sessionID]; ok {
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
