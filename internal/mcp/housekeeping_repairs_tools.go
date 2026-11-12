package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The housekeeping repairs tool is read-only and proxies GET
// /api/autonomous/housekeeping-repairs. It takes no arguments; one that is
// supplied is refused rather than ignored. A group's mode is changed only
// through the existing config set paths.

func (s *Server) toolHousekeepingRepairs() mcpsdk.Tool {
	return mcpsdk.NewTool("housekeeping_repairs",
		mcpsdk.WithDescription("Read the housekeeping repairs: per group (\"task_chain\", \"advance_judge\", \"lane_fill\", \"leftovers\") the mode in effect (\"off\", \"report\" or \"on\"; report records what it would do and acts on nothing), what a report-mode pass would do (\"would\"), what an on-mode pass did (\"did\"), and the cases that failed (\"failed\") or were skipped as unsafe (\"skipped_unsafe\"). Each entry says what was found, what was done and how to undo it. Takes no arguments; any argument is refused. Read-only: reading never repairs anything, raises an alert or changes a mode (change a mode with the config set tool, key autonomous.housekeeping_repairs.<group>). The same object as GET /api/autonomous/housekeeping-repairs."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// HousekeepingRepairsToolNames lists the housekeeping repairs tools, for the parity test.
func HousekeepingRepairsToolNames() []string {
	return []string{"housekeeping_repairs"}
}

func (s *Server) handleHousekeepingRepairs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("housekeeping_repairs",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/housekeeping-repairs", nil)
	if err != nil {
		return toolError("housekeeping_repairs", err), nil
	}
	return textOK(string(out)), nil
}
