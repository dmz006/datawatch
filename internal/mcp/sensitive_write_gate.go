// Sensitive write gate — refuses the sensitive write tool class for
// session and peer callers when mcp.allow_sensitive_writes is false.

package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/federation"
)

// RegisterSensitiveWriteGate installs the sensitive write gate as a
// tool-handler middleware. mcp-go runs the first registered middleware
// outermost, so the gate is registered after the config write audit:
// it sits inside the audit, and the audit sees the denial it returns.
func RegisterSensitiveWriteGate(mcpSrv *server.MCPServer, s *Server) {
	server.WithToolHandlerMiddleware(sensitiveWriteGateMW(s))(mcpSrv)
}

// sensitiveWriteGateMW returns the raw middleware (extracted so tests can
// drive it directly). It refuses a tool for which
// federation.IsSensitiveWriteTool is true when the caller is a session or
// a peer (same caller test as configWriteCallerFrom) and the setting
// mcp.allow_sensitive_writes is off. Admin callers and operator-run PWA,
// REST and CLI calls never reach this path and are unaffected.
func sensitiveWriteGateMW(s *Server) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			if !federation.IsSensitiveWriteTool(req.Params.Name) {
				return next(ctx, req)
			}
			if _, isSessionOrPeer := configWriteCallerFrom(ctx); !isSessionOrPeer {
				return next(ctx, req)
			}
			if s.cfg == nil || s.cfg.SensitiveWritesEnabled() {
				return next(ctx, req)
			}
			return mcpsdk.NewToolResultError(sensitiveWriteDeniedMessage(req.Params.Name)), nil
		}
	}
}

// sensitiveWriteDeniedMessage is the refusal text: it names the setting
// and how an operator re-enables the tool class.
func sensitiveWriteDeniedMessage(tool string) string {
	return "tool " + tool + " is refused: sensitive writes are disabled for sessions and peers " +
		"(mcp.allow_sensitive_writes is false). An operator can re-enable them by setting " +
		"mcp.allow_sensitive_writes to true (or removing the key) in the datawatch config."
}
