// BL463 — MCP error-mode parity with the REST surface.
//
// Before this, MCP tool handlers surfaced failures as success-flagged
// text results (`NewToolResultText("Error: ...")` with a nil error and
// no IsError). To an MCP client such a result is indistinguishable from
// a successful tool call, and through the REST bridge
// (Server.MCPCallJSON, `POST /api/mcp/call`) the same shape comes back
// as HTTP 200 with `isError:false` and an "Error: ..." body — while the
// equivalent direct REST handler returns the real status code (4xx/5xx)
// plus its body. The rest of the codebase already had the correct
// pattern in places (agent_tools / profile_tools / memory_tools use
// mcpsdk.NewToolResultError); these helpers make it one call for every
// handler and carry the operation name plus, for upstream HTTP
// failures, the status and body.

package mcp

import (
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// maxToolErrorBody bounds how much upstream body text is embedded in a
// tool error result so a verbose 5xx dump can't flood the agent's
// context (the REST surface has no practical equivalent of flooding the
// caller's context via the status channel, this is the MCP-side
// analogue of a bounded error body).
const maxToolErrorBody = 2048

// toolError returns a CallToolResult flagged IsError whose text is
// "<op>: <err>". op is the operation name (tool name or the REST path
// the tool proxies); err carries the full detail — for handlers that go
// through proxyGet/proxyJSON that detail already includes the upstream
// status and body ("HTTP %d: <body>"), so MCP clients and the REST
// bridge see the same information the direct REST surface would return
// as a status code + body.
func toolError(op string, err error) *mcpsdk.CallToolResult {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	return mcpsdk.NewToolResultError(fmt.Sprintf("%s: %s", strings.TrimSpace(op), err.Error()))
}

// toolErrorf is toolError for static (non-error) failure messages —
// typically input-validation failures.
func toolErrorf(op, format string, args ...any) *mcpsdk.CallToolResult {
	return toolError(op, fmt.Errorf(format, args...))
}

// toolHTTPError returns a CallToolResult flagged IsError for an upstream
// HTTP failure: operation name, the HTTP status, and the upstream body
// (bounded to maxToolErrorBody). Pass resp=nil / empty body for a
// transport failure; the message degrades to "<op>: HTTP 0: <body>".
func toolHTTPError(op string, resp *http.Response, body []byte) *mcpsdk.CallToolResult {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	detail := strings.TrimSpace(string(body))
	if detail == "" && resp != nil {
		if t := strings.TrimSpace(http.StatusText(status)); t != "" {
			detail = t
		}
	}
	if len(detail) > maxToolErrorBody {
		detail = detail[:maxToolErrorBody] + "…"
	}
	return mcpsdk.NewToolResultError(fmt.Sprintf("%s: HTTP %d: %s", strings.TrimSpace(op), status, detail))
}
