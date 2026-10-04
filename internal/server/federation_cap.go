// BL316 — federation capability enforcement.
//
// Every entry point that a federated peer might reach (REST, MCP, WebSocket,
// comm channels) must call fedCap() before executing the sensitive operation.
// Admin-token requests pass through unconditionally; federated-peer requests
// are gated on the peer's resolved capability set.

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

const (
	fedPeerKey contextKey = iota
	// scopedProxyPeerKey (BL394 §6 iframe-embed follow-up) holds the peer
	// name a request was authenticated against via a short-lived,
	// single-peer-scoped proxy token (see proxy_token.go) rather than the
	// real admin/federation token. Set only by fedAuthMiddleware's own
	// narrow /api/proxy/-only fallback branch below; checked by
	// checkProxyAuth (proxy.go) instead of the normal fedCap path.
	scopedProxyPeerKey
)

// peerFromContext returns the federated peer from the request context,
// or nil if the request is from an admin.
func peerFromContext(ctx context.Context) *multiserver.Entry {
	p, _ := ctx.Value(fedPeerKey).(*multiserver.Entry)
	return p
}

// scopedProxyPeerFromContext returns the peer name a request was
// authenticated against via a scoped proxy token, or "" if it wasn't
// (i.e. it came in via the real admin or federation-peer token instead).
func scopedProxyPeerFromContext(ctx context.Context) string {
	p, _ := ctx.Value(scopedProxyPeerKey).(string)
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
		// BL394 §6 iframe-embed follow-up: a short-lived, single-peer-
		// scoped proxy token (minted by handleRemotePWA, never the real
		// admin/federation token) -- deliberately narrow: only accepted
		// for /api/proxy/ (the proxied page's own API/WS callback calls)
		// and /remote/ (the proxied page's own static asset loads --
		// css/js/images, which a <link>/<script src> tag can no more
		// carry a custom header for than the top-level navigation can),
		// and only if it's bound to the EXACT peer named in that same
		// path, never any other route or peer.
		if tok != "" {
			var peerName string
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/proxy/"):
				peerName = proxyPeerNameFromPath(r.URL.Path)
			case strings.HasPrefix(r.URL.Path, "/remote/"):
				peerName = remotePWAPeerNameFromPath(r.URL.Path)
			}
			if peerName != "" && proxyTokens.valid(tok, peerName) {
				ctx := context.WithValue(r.Context(), scopedProxyPeerKey, peerName)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
