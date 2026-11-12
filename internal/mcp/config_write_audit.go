// BL110 extension (multi-surface audit) — the single audit point for
// every MCP config-write tool.

package mcp

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/federation"
)

// RegisterConfigWriteAudit wires the single audit point for every MCP
// config-write and sensitive-write tool: a tool-handler middleware records
// exactly one federation.ConfigAuditRecord per call, with surface "mcp", for
// the config_set, detection-config-set, dns-channel-config-set and every
// other tool named in federation.IsConfigWriteTool or
// federation.IsSensitiveWriteTool — allowed or denied. The record's Class
// field says which class the tool belongs to.
// Session and peer callers are audited (see configWriteCallerFrom);
// admin callers are not audited as session writes.
func RegisterConfigWriteAudit(mcpSrv *server.MCPServer, s *Server) {
	server.WithToolHandlerMiddleware(configWriteAuditMW(s))(mcpSrv)
}

// configWriteAuditMW returns the raw middleware (extracted so tests can
// drive it directly, the same pattern as the panic-recovery guard). It
// runs after every other tool middleware: the handler has already
// decided allowed vs denied, and the record reflects that decision.
func configWriteAuditMW(s *Server) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (res *mcpsdk.CallToolResult, err error) {
			res, err = next(ctx, req)
			class := auditToolClass(req.Params.Name)
			if s.configAudit == nil || class == "" {
				return res, err
			}
			caller, ok := configWriteCallerFrom(ctx)
			if !ok {
				return res, err // admin — not audited as a session write
			}
			allowed := err == nil && (res == nil || !res.IsError)
			rec := federation.ConfigAuditRecord{
				Surface: "mcp",
				Tool:    req.Params.Name,
				Class:   class,
				Caller:  caller,
				Allowed: allowed,
			}
			if args, ok := req.Params.Arguments.(map[string]any); ok {
				if raw, merr := json.Marshal(args); merr == nil {
					rec.Keys, rec.Values = federation.ExtractConfigPatch(raw)
					if class == auditClassSensitive {
						// Sensitive tools carry secret material in arguments
						// that key-name redaction cannot recognise; record
						// the argument names only, never the values.
						rec.Values = nil
					}
				}
			}
			if !allowed {
				rec.Reason = configWriteDenyReason(err, res)
			}
			s.configAudit.Record(rec)
			return res, err
		}
	}
}

// Audit tool classes carried in ConfigAuditRecord.Class.
const (
	auditClassConfig    = "config"
	auditClassSensitive = "sensitive"
)

// auditToolClass returns the audit class of an MCP tool, or "" when the
// tool is in neither audited class.
func auditToolClass(tool string) string {
	switch {
	case federation.IsConfigWriteTool(tool):
		return auditClassConfig
	case federation.IsSensitiveWriteTool(tool):
		return auditClassSensitive
	}
	return ""
}

// configWriteCallerFrom resolves the audit caller identity for an MCP
// call: the federation peer (BL317 SSE auth) or the session ID of a
// scoped token (HLLM-004 REST bridge). ok=false means admin — an
// unattributed write is not audited as a session write.
func configWriteCallerFrom(ctx context.Context) (string, bool) {
	if peer := mcpPeerFromContext(ctx); peer != nil {
		return peer.Name, true
	}
	if id := federation.CallerSessionIDFromContext(ctx); id != "" {
		return id, true
	}
	return "", false
}

// configWriteDenyReason gives the audit sink the denial reason: the
// handler error, else the first text content of the error result,
// bounded to 300 runes.
func configWriteDenyReason(err error, res *mcpsdk.CallToolResult) string {
	if err != nil {
		return bounded(err.Error(), 300)
	}
	if res != nil {
		for _, c := range res.Content {
			if tc, ok := c.(mcpsdk.TextContent); ok {
				return bounded(tc.Text, 300)
			}
		}
	}
	return "denied"
}

// bounded truncates s to at most n runes without splitting a UTF-8
// sequence.
func bounded(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
