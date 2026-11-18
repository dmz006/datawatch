package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// restart_interruptions is read-only and proxies
// GET /api/autonomous/restart-interruptions.

func (s *Server) toolRestartInterruptions() mcpsdk.Tool {
	return mcpsdk.NewTool("restart_interruptions",
		mcpsdk.WithDescription("Report what a daemon restart now would interrupt: the running tasks (executing worker or verifying) and the live sessions, with their ids and counts, the running final gates (running_gates, running_gate_count; a restart or stop waits for them up to autonomous.gate_drain_minutes), plus the configured restart policy (autonomous.restart_policy: immediate, wait_quiet or drain). Read-only; the same object as GET /api/autonomous/restart-interruptions."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleRestartInterruptions(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/restart-interruptions", nil)
	if err != nil {
		return toolError("restart_interruptions", err), nil
	}
	return textOK(string(out)), nil
}
