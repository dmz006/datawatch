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
		mcpsdk.WithDescription("Read the supervisor watch state: the mode in effect (\"off\", \"shadow\" or \"on\"; an unset mode reads shadow, which reports only and never acts), the instant the coordinator last polled (\"last_poll_at\"), the stored findings (\"findings\", open first, most recently seen first: kind, subject, the operator line, first and last seen, the shadow would-alert mark and the raised-alert instant), the comparison against the supervisor script's record (\"comparison\") and the per-band wait stats (\"priority_bands\": one row per band that waits at least one Automaton — high (priority 80 or more), medium (50 through 79) and low (below 50) — with the band's waiting count, its oldest wait in whole minutes and how many of those waiters also satisfy the watch's priority-stall rule; an empty list when no band waits anything). Takes no arguments; any argument is refused. Read-only: listing never raises an alert, resolves a finding or changes the mode. The same object as GET /api/autonomous/supervisor-watch."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolSupervisorWatchStats() mcpsdk.Tool {
	return mcpsdk.NewTool("supervisor_watch_stats",
		mcpsdk.WithDescription("Read the supervisor watch numbers: the mode in effect (\"mode\"), when the coordinator last polled (\"last_poll_at\") and how many seconds ago (\"last_poll_age_seconds\", -1 when it never has), the unresolved findings (\"findings_open\", of which \"findings_would_alert\" shadow marked alert-due and \"findings_alerted\" raised an alert), the resolved ones (\"findings_resolved\"), the days of full agreement with the supervisor script (\"agreed_days\") and the per-band wait stats (\"priority_bands\": the same band rows the list view carries — high, medium, low order, waiting count, oldest wait in minutes and the band's stalled share). Takes no arguments; any argument is refused. Read-only. The same object as GET /api/autonomous/supervisor-watch/stats."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// SupervisorWatchToolNames lists the supervisor watch tools, for the parity test.
func SupervisorWatchToolNames() []string {
	return []string{"supervisor_watch_list", "supervisor_watch_stats", "priority_stalls"}
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

func (s *Server) toolPriorityStalls() mcpsdk.Tool {
	return mcpsdk.NewTool("priority_stalls",
		mcpsdk.WithDescription("Read the current priority stalls: each waiting Automaton at or above the priority floor that has waited past the stall threshold, with its id (\"automaton_id\"), priority, wait in minutes, the blocker code (\"blocker_code\"), the blocker text (\"blocker_text\") and the one action that clears it (\"action\"), high priority first. Takes no arguments; any argument is refused. Read-only: it never wakes the supervisor and never approves, runs or acknowledges an Automaton. The same object as GET /api/autonomous/priority-stalls."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handlePriorityStalls(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolError("priority_stalls",
			fmt.Errorf("unknown argument(s): %s; this tool takes no arguments", strings.Join(names, ", "))), nil
	}
	out, err := s.proxyGet("/api/autonomous/priority-stalls", nil)
	if err != nil {
		return toolError("priority_stalls", err), nil
	}
	return textOK(string(out)), nil
}
