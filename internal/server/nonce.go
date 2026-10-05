// SEC-006 — short-lived, single-use nonces for the handful of browser
// requests that cannot attach an Authorization header: an <a href> /
// top-level navigation (file download/view links, the remote-peer-PWA
// viewer) and an EventSource connect (the Signal link-flow stream). Minting
// a nonce itself requires an already-authenticated fetch/XHR call (which
// CAN set the header); the nonce then rides in the URL as a one-time,
// short-lived stand-in for that caller's own token — never the token
// itself. A leaked nonce (server access log, Referer header, browser
// history) is usable once, for a short window, and only on the specific
// nonce-aware routes below — not a general-purpose bearer credential like
// the ?token= fallback it replaces.

package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const nonceTTL = 60 * time.Second

type nonceEntry struct {
	// token is the real bearer token (admin or federation peer) this
	// nonce stands in for — the nonce-aware handlers treat a valid,
	// unexpired nonce as equivalent to presenting this token via the
	// normal Authorization header.
	token     string
	expiresAt time.Time
}

// nonceStore mints and single-use-consumes SEC-006 nonces. Zero value is
// usable; call startSweeper once to reap expired entries.
type nonceStore struct {
	mu      sync.Mutex
	entries map[string]nonceEntry
}

func newNonceStore() *nonceStore {
	return &nonceStore{entries: make(map[string]nonceEntry)}
}

// Mint creates a new nonce standing in for forToken, valid for nonceTTL.
func (n *nonceStore) Mint(forToken string) (string, time.Time) {
	buf := make([]byte, 20)
	_, _ = rand.Read(buf) // crypto/rand.Read never errors on Linux/Darwin/Windows per stdlib docs
	nonce := hex.EncodeToString(buf)
	expiresAt := time.Now().Add(nonceTTL)

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.entries == nil {
		n.entries = make(map[string]nonceEntry)
	}
	n.entries[nonce] = nonceEntry{token: forToken, expiresAt: expiresAt}
	return nonce, expiresAt
}

// Consume returns the token a nonce stands in for and deletes it — a
// nonce is usable exactly once, whether or not that use succeeds, so a
// captured-and-replayed nonce can never be used twice even if the first
// use is still in flight.
func (n *nonceStore) Consume(nonce string) (token string, ok bool) {
	if nonce == "" {
		return "", false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	e, found := n.entries[nonce]
	if !found {
		return "", false
	}
	delete(n.entries, nonce)
	if time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.token, true
}

// sweep removes expired entries so a flood of minted-but-never-consumed
// nonces doesn't grow the map unboundedly (SEC-018's own lesson, applied
// up front instead of found live later).
func (n *nonceStore) sweep() {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for k, e := range n.entries {
		if now.After(e.expiresAt) {
			delete(n.entries, k)
		}
	}
}

// startSweeper runs sweep on interval until stop is closed.
func (n *nonceStore) startSweeper(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-t.C:
				n.sweep()
			case <-stop:
				return
			}
		}
	}()
}

// handleAuthNonce mints a SEC-006 nonce standing in for the caller's own
// already-authenticated token (the Authorization header got this request
// past fedAuthMiddleware in the first place — the nonce exists so a
// SUBSEQUENT plain-GET request, which can't carry that header, can
// authenticate too). POST only; no body.
//
// POST /api/auth/nonce -> {"nonce": "...", "expires_at": "2026-10-05T12:00:00Z"}
func (s *Server) handleAuthNonce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.nonces == nil {
		http.Error(w, "nonce store not initialized", http.StatusServiceUnavailable)
		return
	}
	tok := callerTokenFromContext(r.Context())
	if tok == "" {
		// Either server.token is empty (fedAuthMiddleware passed
		// everyone through with no identity to mint a nonce for — there's
		// nothing a nonce would add over just not sending a token at
		// all), or the caller came in via the scoped-proxy-token
		// fallback (which is already its own short-lived, narrow
		// credential — minting a nonce for a nonce-like thing adds
		// nothing). Either way, a nonce is meaningless here.
		http.Error(w, "no caller token to mint a nonce for", http.StatusBadRequest)
		return
	}
	nonce, expiresAt := s.nonces.Mint(tok)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"nonce":      nonce,
		"expires_at": expiresAt,
	})
}
