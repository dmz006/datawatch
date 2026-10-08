package federation

import "context"

// HLLM-004 — the shared daemon MCP server (internal/mcp.Server) is a single
// long-lived instance that serves every caller (admin, federation peers, AND
// every spawned session's own scoped credential) through one dispatch path:
// internal/server's handleMCPCall -> mcpBridgeAPI.MCPCallJSON(ctx, ...). That
// server's own callerSessionID field is only ever set once, for the separate
// "Goose channel" subprocess case (datawatch mcp --caller-session-id=X) — it
// is NOT per-call, so it can't identify which session issued any given call
// through the shared instance.
//
// internal/mcp already imports internal/federation (for capability checks),
// and internal/server imports internal/federation too, so this package is a
// cycle-free place for internal/server to stash the real per-call caller
// session ID (resolved from the session token that authenticated the
// request, via auth.SessionTokenStore.SessionIDForToken) and for
// internal/mcp's tool handlers to read it back out, instead of a direct
// context key exchange that would require one package to import the other.

type callerSessionCtxKey struct{}

// WithCallerSessionID returns a context carrying the FullID of the session
// whose scoped credential authenticated the current request. Call sites
// that didn't authenticate via a session token (admin, federation peer)
// should not call this — CallerSessionIDFromContext returning "" means
// "no per-call session identity; the static/subprocess field, if any,
// applies instead."
func WithCallerSessionID(ctx context.Context, sessionFullID string) context.Context {
	if sessionFullID == "" {
		return ctx
	}
	return context.WithValue(ctx, callerSessionCtxKey{}, sessionFullID)
}

// CallerSessionIDFromContext returns the per-call caller session ID stashed
// by WithCallerSessionID, or "" if none is set.
func CallerSessionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(callerSessionCtxKey{}).(string)
	return id
}
