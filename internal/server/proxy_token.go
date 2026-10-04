// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-
// review.md §6, iframe-embed follow-up) — the proxy sandbox origin fixed
// token theft by giving a proxied peer's PWA its own, empty localStorage,
// but that also means it has no token for its OWN API calls back through
// /api/proxy/{peer}/... (tokenHeader() reads localStorage, which is now
// genuinely empty on that origin). Injecting the REAL admin token to fix
// that would undo the whole point of the origin split -- a compromised
// peer's JS could read it right back out.
//
// The fix: a short-lived, single-peer-scoped token, minted server-side
// only after the real admin/federation auth already passed (see
// handleRemotePWA), and valid for nothing except /api/proxy/{that one
// peer}/... . If a compromised peer's JS reads it out of localStorage and
// exfiltrates it, the blast radius is "can proxy to this one
// already-approved peer, for up to an hour" -- not "full admin access,
// indefinitely."

package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

const proxyTokenTTL = time.Hour

type proxyTokenEntry struct {
	peerName  string
	expiresAt time.Time
}

type proxyTokenStore struct {
	mu     sync.Mutex
	tokens map[string]proxyTokenEntry
}

var proxyTokens = &proxyTokenStore{tokens: make(map[string]proxyTokenEntry)}

// mint generates a new random token scoped to peerName, valid for
// proxyTokenTTL. Also opportunistically sweeps expired entries so the
// map doesn't grow unbounded across long daemon uptimes -- this is a
// low-volume operation (once per /remote/ page load, not per API call),
// so an O(n) sweep on every mint is cheap.
func (s *proxyTokenStore) mint(peerName string) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "" // caller skips injecting a token; proxied page just has none, same as before this feature existed
	}
	tok := hex.EncodeToString(b)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.tokens {
		if now.After(v.expiresAt) {
			delete(s.tokens, k)
		}
	}
	s.tokens[tok] = proxyTokenEntry{peerName: peerName, expiresAt: now.Add(proxyTokenTTL)}
	return tok
}

// valid reports whether token is a live, unexpired scoped token bound to
// exactly peerName. A token minted for peer "a" is never valid for peer
// "b" -- the whole point is that it isn't a general-purpose credential.
func (s *proxyTokenStore) valid(token, peerName string) bool {
	if token == "" || peerName == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.tokens, token)
		return false
	}
	return entry.peerName == peerName
}

// checkProxyAuth gates handleProxy/handleProxyWS. If the request was
// authenticated via a scoped proxy token (fedAuthMiddleware tagged the
// context), it's authorized only if that token's bound peer matches
// serverName exactly -- defense in depth, since fedAuthMiddleware already
// verified this before tagging the context, but a future refactor
// shouldn't be able to silently widen a scoped token's reach by accident.
// Otherwise falls back to the normal admin/federation-capability check,
// completely unchanged from before this feature existed.
func (s *Server) checkProxyAuth(w http.ResponseWriter, r *http.Request, serverName string) bool {
	if scopedPeer := scopedProxyPeerFromContext(r.Context()); scopedPeer != "" {
		if scopedPeer == serverName {
			return true
		}
		http.Error(w, "scoped proxy token is not valid for this server", http.StatusForbidden)
		return false
	}
	return s.fedCap(w, r, federation.CapConfigRead)
}

// proxyPeerNameFromPath extracts the {serverName} segment from an
// /api/proxy/{serverName}/... path, the same parsing handleProxy itself
// does, duplicated here (cheaply -- this runs once per request, string
// ops only) so fedAuthMiddleware can check a scoped token's bound peer
// before handleProxy is ever reached.
func proxyPeerNameFromPath(path string) string {
	rest := strings.TrimPrefix(path, "/api/proxy/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// remotePWAPeerNameFromPath is proxyPeerNameFromPath's counterpart for
// /remote/{serverName}/... -- the proxied page's own static assets
// (css/js/images, rewritten by rewritePWAContent to live under this
// prefix) are gated the same way the top-level page is, and need the
// same scoped-token fallback.
func remotePWAPeerNameFromPath(path string) string {
	rest := strings.TrimPrefix(path, "/remote/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// appendProxyToken adds a ?token=/&token= query param to url (whichever
// is correct given url may already carry a query string, e.g. the
// "?v=<version>" cache-buster on rewritten asset URLs). A no-op if token
// is empty, so a mint() failure degrades to exactly today's (broken,
// pre-this-fix) behavior rather than producing a malformed URL.
func appendProxyToken(url, token string) string {
	if token == "" {
		return url
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return url + sep + "token=" + token
}
