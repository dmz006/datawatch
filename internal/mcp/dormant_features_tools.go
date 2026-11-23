package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The dormant features tool is read-only and proxies GET
// /api/autonomous/dormant-features. It takes no arguments; one that is
// supplied is refused rather than ignored. Nothing here turns a feature on:
// a setting is changed only through the existing config set paths.

func (s *Server) toolAutonomousDormantFeatures() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_dormant_features",
		mcpsdk.WithDescription("List the dormant features: the shipped Automaton features that are switched off or unused on this daemon, each with what it does and where it is configured. Lists only; it never changes a setting, starts a feature or runs any Automaton. Takes no arguments; any argument is refused. Read-only: change a feature's setting with the config set tool. The same object as GET /api/autonomous/dormant-features."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// DormantFeaturesToolNames lists the dormant features tools, for the parity test.
func DormantFeaturesToolNames() []string {
	return []string{"autonomous_dormant_features"}
}

func (s *Server) handleAutonomousDormantFeatures(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("autonomous_dormant_features",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/dormant-features", nil)
	if err != nil {
		return toolError("autonomous_dormant_features", err), nil
	}
	return textOK(string(out)), nil
}
