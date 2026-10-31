package mcp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The infrastructure retry tool is read-only and proxies
// GET /api/autonomous/infra-retries. The retry policy itself is set through
// the existing config tools (config_set with autonomous.infra_retry.* keys).

func (s *Server) toolAutonomousInfraRetries() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_infra_retries",
		mcpsdk.WithDescription("Read the infrastructure retry state: the retry policy in effect and the tasks of Automata that were retried, or are held, because the machinery around them failed (a spawn error, a killed session, a daemon restart or a storage write) rather than because of their own work. The most recently retried task comes first. Read-only; same object as GET /api/autonomous/infra-retries. automaton limits the list to one Automaton; limit sets how many tasks come back (a whole number from 1 to 1000, default 50)."),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional — an Automaton ID; only its tasks are listed")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Optional — how many tasks to return, a whole number from 1 to 1000 (default 50)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousInfraRetries(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	args := req.GetArguments()
	if v, ok := args["automaton"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("automaton", strings.TrimSpace(v))
	}
	// limit arrives as a number, or as text from a client that quotes it; the
	// REST route decides whether it is valid.
	switch v := args["limit"].(type) {
	case string:
		if v != "" {
			q.Set("limit", v)
		}
	case float64:
		q.Set("limit", strconv.FormatFloat(v, 'f', -1, 64))
	}
	out, err := s.proxyGet("/api/autonomous/infra-retries", q)
	if err != nil {
		return toolError("autonomous_infra_retries", err), nil
	}
	return textOK(string(out)), nil
}
