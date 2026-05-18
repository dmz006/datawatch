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

// mcpFedAuthMiddleware wraps an http.Handler with combined admin+federation auth.
//
//   - Admin token → pass through unchanged.
//   - Known federation peer token → tag context with the peer Entry; downstream
//     handlers call mcpFedCap to enforce per-capability checks.
//   - Unknown token → 401.
//   - No token and no admin required (s.cfg.Token == "") → pass through.
func (s *Server) mcpFedAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}

		// No auth configured — open access.
		if s.cfg.Token == "" && s.fedPeerStore == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Admin token.
		if s.cfg.Token != "" && tok == s.cfg.Token {
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
