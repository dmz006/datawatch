package mcp

import (
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// tracePackOpt is the optional pack argument shared by the trace tools that
// report per-event pack attribution (trace_session, trace_rollup).
func tracePackOpt() mcpsdk.ToolOption {
	return mcpsdk.WithString("pack", mcpsdk.Description("Optional — keep only events attributed to this installable pack (pack_name)"))
}

// addTracePackQuery copies the optional pack argument of req into q as
// ?pack=. A blank or absent argument leaves q unchanged.
func addTracePackQuery(req mcpsdk.CallToolRequest, q url.Values) url.Values {
	if v, ok := req.GetArguments()["pack"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("pack", strings.TrimSpace(v))
	}
	return q
}
