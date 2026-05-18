// BL316 — federation capability enforcement.
//
// Every entry point that a federated peer might reach (REST, MCP, WebSocket,
// comm channels) must call fedCap() before executing the sensitive operation.
// Admin-token requests pass through unconditionally; federated-peer requests
// are gated on the peer's resolved capability set.
//
// fedPeerFrom() identifies the caller by matching the Bearer token against the
// serverStore. The admin token is never stored in the serverStore, so an admin
// request always returns nil and bypasses capability enforcement.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

type contextKey int

const fedPeerKey contextKey = iota

// fedPeerFrom returns the federation peer Entry whose Token matches the
// request's Bearer token, or nil if the request carries the admin token or
// an unknown token.
func (s *Server) fedPeerFrom(r *http.Request) *multiserver.Entry {
	if s.serverStore == nil {
		return nil
	}
	tok := r.URL.Query().Get("token")
	if tok == "" {
		auth := r.Header.Get("Authorization")
		tok = strings.TrimPrefix(auth, "Bearer ")
	}
	if tok == "" || tok == s.token {
		return nil
	}
	peer, ok := s.serverStore.GetByToken(tok)
	if !ok || !peer.Federated {
		return nil
	}
	return peer
}

// fedCapMiddleware extends the auth middleware to also accept federation peer
// tokens. It tags the request context with the peer Entry so downstream
// handlers can call fedCap() without re-querying the store.
//
// Note: this middleware runs AFTER authMiddleware, which already rejected
// requests with no valid token. Here we re-examine requests that carried a
// non-admin token and accept them if they match a federated peer.
func (s *Server) fedCapMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.serverStore == nil || s.token == "" {
			next.ServeHTTP(w, r)
			return
		}
		tok := r.URL.Query().Get("token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
		// Admin token — pass through (authMiddleware already validated it).
		if tok == s.token {
			next.ServeHTTP(w, r)
			return
		}
		// Check if this is a known federated peer token.
		peer, ok := s.serverStore.GetByToken(tok)
		if !ok || !peer.Federated {
			// authMiddleware already rejected non-admin unknown tokens with 401;
			// we should not reach here in normal flow. Pass through to let
			// authMiddleware's response stand.
			next.ServeHTTP(w, r)
			return
		}
		// Tag context with the peer so handlers can enforce capabilities.
		ctx := context.WithValue(r.Context(), fedPeerKey, peer)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// peerFromContext returns the federated peer from the request context,
// or nil if the request is from an admin.
func peerFromContext(ctx context.Context) *multiserver.Entry {
	p, _ := ctx.Value(fedPeerKey).(*multiserver.Entry)
	return p
}

// fedCap checks whether the request's caller has the required capability.
// Admin requests (nil peer) always pass. Federated peer requests are checked
// against the peer's resolved capability set.
//
// Returns true if the caller is authorized; false if not (and a 403 has been
// written to w).
func (s *Server) fedCap(w http.ResponseWriter, r *http.Request, required string) bool {
	peer := peerFromContext(r.Context())
	if peer == nil {
		return true // admin — unrestricted
	}
	var custom map[string]*federation.CapabilityGroup
	if s.fedGroupStore != nil {
		custom = s.fedGroupStore.AsMap()
	}
	resolved := federation.Resolve(peer.Capabilities, custom)
	if !federation.Check(resolved, required) {
		http.Error(w, "federation peer lacks capability: "+required, http.StatusForbidden)
		return false
	}
	return true
}

// mustMarshal marshals v to JSON, returning nil on error.
// Used by WS handlers that need inline JSON without an error branch.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// fedAuthMiddleware is a combined auth+federation middleware that replaces
// the plain authMiddleware for the main mux. It:
//  1. Accepts the admin token (pass through).
//  2. Accepts a federated peer token (tag context with peer).
//  3. Rejects everything else with 401.
func (s *Server) fedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			next.ServeHTTP(w, r)
			return
		}
		tok := r.URL.Query().Get("token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
		// Admin token.
		if tok == s.token {
			next.ServeHTTP(w, r)
			return
		}
		// Federation peer token.
		if s.serverStore != nil && tok != "" {
			peer, ok := s.serverStore.GetByToken(tok)
			if ok && peer.Federated {
				ctx := context.WithValue(r.Context(), fedPeerKey, peer)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
