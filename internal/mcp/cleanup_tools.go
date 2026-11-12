package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The cleanup tool is read-only. autonomous_cleanup_status proxies
// GET /api/autonomous/cleanup: both cleanup passes with their last run and
// the owner-vs-net split, the two disk readings, and the rate-limited disk
// alert flag. It never removes anything.

func (s *Server) toolAutonomousCleanupStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_cleanup_status",
		mcpsdk.WithDescription("Read the Automaton automation cleanup status: the worktrees pass (\"worktrees\") and the daemon temp-file sweep (\"temp\"), each with its mode (\"mode\", off|report|on), last run (\"last_run\"), the removed / would-remove / skipped / failed counts (\"removed\", \"would_remove\", \"skipped\", \"failed\"), the bytes freed (\"bytes_freed\"), the last error (\"last_error\") and the owner-vs-net split (\"owner_removed\", \"net_removed\", \"owner_bytes_freed\", \"net_bytes_freed\"); the two disk readings (\"worktree_disk\", \"temp_disk\") with their usage percent when the filesystem was measurable; and the disk-alert threshold (\"disk_alert_percent\"), the rate-limited alert flag (\"disk_alert\") and the last time the alert actually raised (\"last_disk_alert_at\"). Read-only; the same object as GET /api/autonomous/cleanup. Nothing is removed."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// CleanupToolNames lists the cleanup tools, for the parity test.
func CleanupToolNames() []string {
	return []string{"autonomous_cleanup_status"}
}

func (s *Server) handleAutonomousCleanupStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/cleanup", url.Values{})
	if err != nil {
		return toolError("autonomous_cleanup_status", err), nil
	}
	return textOK(string(out)), nil
}
