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
// supplied is refused rather than ignored.

func (s *Server) toolAutonomousDormantFeatures() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_dormant_features",
		mcpsdk.WithDescription("List the Automata features that are shipped but currently dormant (switched off or in report mode), so an operator can see what is available to turn on. Takes no arguments; any argument is refused. Read-only: it only lists and never changes a setting (change a setting with the config set tool). The same object as GET /api/autonomous/dormant-features."),
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

// errNoArguments is the refusal for a tool that takes no arguments.
func errNoArguments(args map[string]any) error {
	names := make([]string, 0, len(args))
	for k := range args {
		names = append(names, k)
	}
	sort.Strings(names)
	return fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))
}
