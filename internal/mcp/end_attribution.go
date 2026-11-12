package mcp

import "context"

// Callers name the MCP tool that ended a session. The end reason names who
// made the call: the session that owns the caller's scoped token, or the
// operator.
const (
	endCallerMCPKillSession = "mcp.kill-session"
	endCallerMCPStopAll     = "mcp.stop-all-sessions"
)

// endActor returns who an MCP call acts for, for an end reason. A call made
// with a session-scoped caller names that session; anything else is the
// operator, which is how the MCP surface names its unscoped caller.
func (s *Server) endActor(ctx context.Context) string {
	if id, scoped := s.scopedCallerSessionID(ctx); scoped && id != "" {
		return "session:" + id
	}
	return "operator"
}
