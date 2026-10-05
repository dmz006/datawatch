// BL317 — federated authentication and capability enforcement for MCP SSE transport.
//
// The MCP SSE server runs on its own port and was previously gated only by a
// static bearer token.  This file adds:
//
//   1. A FedPeerStore interface so the MCP package can look up federation peers
//      without importing the full server/multiserver package (though it can).
//   2. mcpFedAuthMiddleware — replaces bearerAuthMiddleware in ServeSSE.
//      Accepts the admin token (full access) or a registered federation peer token
//      (access gated by capabilities).
//   3. mcpFedCap — per-request capability guard for MCP tool handlers.
//      Returns an MCP error result when the peer lacks the required capability.

package mcp

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// FedPeerStore is the narrow interface the MCP server needs to look up
// federation peer entries by bearer token.
type FedPeerStore interface {
	GetByToken(tok string) (*multiserver.Entry, bool)
}

type mcpFedContextKey int

const mcpFedPeerKey mcpFedContextKey = 0

// constantTimeEqual reports whether a and b are equal, in constant time
// when they're the same length (an explicit length check first is not
// itself a meaningful timing leak — only byte-content comparison is).
func constantTimeEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// effectiveToken is the admin credential that gates MCP SSE: mcp.token
// when set, otherwise server.token (SEC-002 — mcp.token being empty must
// not mean "MCP SSE is open" when the operator has protected REST with a
// server.token; see Options.FallbackToken).
func (s *Server) effectiveToken() string {
	if s.cfg.Token != "" {
		return s.cfg.Token
	}
	return s.fallbackToken
}

// mcpFedAuthMiddleware wraps an http.Handler with combined admin+federation auth.
//
//   - Admin token (mcp.token, or server.token when mcp.token is empty) → pass through.
//   - Known federation peer token → tag context with the peer Entry; downstream
//     handlers call mcpFedCap to enforce per-capability checks.
//   - Unknown token → 401.
//   - No admin token resolvable at all (both mcp.token and server.token empty)
//     → pass through; this is the operator's explicit, documented choice (SEC-001).
func (s *Server) mcpFedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
		admin := s.effectiveToken()

		// No admin token resolvable — open access (federation peers still
		// tagged in context downstream, but not required for basic connectivity).
		if admin == "" {
			if s.fedPeerStore != nil && tok != "" {
				if peer, ok := s.fedPeerStore.GetByToken(tok); ok && peer.Federated {
					ctx := context.WithValue(r.Context(), mcpFedPeerKey, peer)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}

		// Admin token.
		if tok != "" && constantTimeEqual(tok, admin) {
			next.ServeHTTP(w, r)
			return
		}

		// Federation peer token.
		if s.fedPeerStore != nil && tok != "" {
			peer, ok := s.fedPeerStore.GetByToken(tok)
			if ok && peer.Federated {
				ctx := context.WithValue(r.Context(), mcpFedPeerKey, peer)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// mcpPeerFromContext returns the federation peer from an MCP handler context,
// or nil when the caller used the admin token.
func mcpPeerFromContext(ctx context.Context) *multiserver.Entry {
	p, _ := ctx.Value(mcpFedPeerKey).(*multiserver.Entry)
	return p
}

// mcpFedCap checks whether the peer in ctx holds the required capability.
// Admin callers (nil peer) always pass.  Returns nil on success, or an MCP
// CallToolResult with an error message when the capability check fails.
func mcpFedCap(ctx context.Context, required string) *mcpsdk.CallToolResult {
	peer := mcpPeerFromContext(ctx)
	if peer == nil {
		return nil // admin — unrestricted
	}
	resolved := federation.Resolve(peer.Capabilities, nil)
	if !federation.Check(resolved, required) {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{
				mcpsdk.TextContent{
					Type: "text",
					Text: fmt.Sprintf("federation peer lacks capability: %s", required),
				},
			},
		}
	}
	return nil
}
