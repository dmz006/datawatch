package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The supervisor watch list tool is read-only and proxies GET
// /api/autonomous/supervisor-watch: the watch's mode in effect, the
// instant the coordinator last polled, the stored findings (open first,
// most recently seen first) and the comparison against the script's
// record. It takes no arguments; one that is supplied is refused rather
// than ignored, so a caller that believes it is filtering finds out.

func (s *Server) toolSupervisorWatchList() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_watch_list",
		mcpsdk.WithDescription("Read the supervisor watch state: the mode in effect (\"off\", \"shadow\" or \"on\"; an unset mode reads shadow, which reports only and never acts), the instant the coordinator last polled (\"last_poll_at\"), the stored findings (\"findings\", open first, most recently seen first: kind, subject, the operator line, first and last seen, the shadow would-alert mark and the raised-alert instant) and the comparison against the supervisor script's record (\"comparison\"). Takes no arguments; any argument is refused. Read-only: listing never raises an alert, resolves a finding or changes the mode. The same object as GET /api/autonomous/supervisor-watch."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolSupervisorWatchStats() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_watch_stats",
		mcpsdk.WithDescription("Read the supervisor watch numbers: the mode in effect (\"mode\"), when the coordinator last polled (\"last_poll_at\") and how many seconds ago (\"last_poll_age_seconds\", -1 when it never has), the unresolved findings (\"findings_open\", of which \"findings_would_alert\" shadow marked alert-due and \"findings_alerted\" raised an alert), the resolved ones (\"findings_resolved\") and the days of full agreement with the supervisor script (\"agreed_days\"). Takes no arguments; any argument is refused. Read-only. The same object as GET /api/autonomous/supervisor-watch/stats."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// SupervisorWatchToolNames lists the supervisor watch tools, for the parity test.
func SupervisorWatchToolNames() []string {
	return []string{"supervisor_watch_list", "supervisor_watch_stats"}
}

func (s *Server) handleSupervisorWatchList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("supervisor_watch_list",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/supervisor-watch", nil)
	if err != nil {
		return toolError("supervisor_watch_list", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleSupervisorWatchStats(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("supervisor_watch_stats",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/supervisor-watch/stats", nil)
	if err != nil {
		return toolError("supervisor_watch_stats", err), nil
	}
	return textOK(string(out)), nil
}
