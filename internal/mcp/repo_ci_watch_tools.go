package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The repo CI watch tool is read-only and proxies GET
// /api/autonomous/repo-ci-watch: the watch state — the mode in effect,
// the alert threshold, the watched repository paths, the last poll
// instant — beside the stored watch records, newest first.

func (s *Server) toolAutonomousRepoCIWatch() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_repo_ci_watch",
		mcpsdk.WithDescription("Read the repo CI watch state: the mode in effect (\"off\", \"shadow\" or \"on\"), the alert threshold in whole minutes (\"alert_minutes\", 0 while the watch is off), the watched repository paths (\"repos\"), the instant the coordinator last polled (\"last_poll_at\") and the stored watch records (\"records\", newest first). Optional: repo (only that repository's records), status (\"green\", \"failing\", \"pending\", \"hung\", \"rerunning\" or \"superseded\" only). Read-only; the same object as GET /api/autonomous/repo-ci-watch."),
		mcpsdk.WithString("repo",
			mcpsdk.Description(`Optional — only the records of this repository path (e.g. "dmz006/datawatch").`)),
		mcpsdk.WithString("status",
			mcpsdk.Description(`Optional — only records in this watch state: "green", "failing", "pending", "hung", "rerunning" or "superseded" (anything else is refused).`)),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// RepoCIWatchToolNames lists the repo CI watch tools, for the parity test.
func RepoCIWatchToolNames() []string {
	return []string{"autonomous_repo_ci_watch"}
}

func (s *Server) handleAutonomousRepoCIWatch(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("repo", ""); v != "" {
		q.Set("repo", v)
	}
	if v := req.GetString("status", ""); v != "" {
		q.Set("status", v)
	}
	out, err := s.proxyGet("/api/autonomous/repo-ci-watch", q)
	if err != nil {
		return toolError("autonomous_repo_ci_watch", err), nil
	}
	return textOK(string(out)), nil
}
