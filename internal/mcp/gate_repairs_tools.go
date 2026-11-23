package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The gate repair tool is read-only and proxies GET
// /api/autonomous/gate-repairs. It takes no arguments; one that is supplied
// is refused rather than ignored. The engine mode is changed only through
// the existing config set paths (autonomous.gate_repair.mode).

func (s *Server) toolGateRepairs() mcpsdk.Tool {
	return mcpsdk.NewTool("gate_repairs",
		mcpsdk.WithDescription("Read the gate repair engine: the mode in effect (\"off\", \"report\" or \"on\"), its records newest first (for each failed gate: the failure class found, what it would do or did, and how to undo it), and how much repair budget has been spent. Takes no arguments; any argument is refused. Read-only: reading never repairs anything, raises an alert or changes a mode (change the mode with the config set tool, key autonomous.gate_repair.mode). The same object as GET /api/autonomous/gate-repairs."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// GateRepairsToolNames lists the gate repair tools, for the parity test.
func GateRepairsToolNames() []string {
	return []string{"gate_repairs"}
}

func (s *Server) handleGateRepairs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("gate_repairs",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/gate-repairs", nil)
	if err != nil {
		return toolError("gate_repairs", err), nil
	}
	return textOK(string(out)), nil
}
