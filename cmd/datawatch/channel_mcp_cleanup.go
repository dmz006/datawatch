package main

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/channel"
)

// cleanupStaleChannelMCP removes old channel registrations from claude's MCP
// list. It runs once per daemon start, in its own goroutine: it runs the claude
// CLI, and `claude mcp list` contacts every MCP server the user configured, so
// one unreachable server holds it for that client's whole connect timeout. A
// session is saved before its registration is added, so a session that starts
// while this runs is never taken for a stale one.
func cleanupStaleChannelMCP(usingGoBridge bool, hostname string, sessionExists func(string) bool) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Printf("[channel] MCP registration cleanup panic (recovered): %v\n", p)
		}
	}()
	if usingGoBridge {
		// BL288 (v5.4.0) — purge stale `datawatch*` entries in claude's
		// MCP list that still point at `node + channel.js` from before
		// the Go-bridge migration.
		if cleaned := channel.CleanupStaleJSRegistrations(); len(cleaned) > 0 {
			fmt.Printf("[channel] removed stale JS-bridge MCP registration(s): %s\n", strings.Join(cleaned, ", "))
		}
	}
	// Remove stale global "datawatch" MCP if it exists (replaced by per-session servers)
	channel.UnregisterGlobalMCP()
	// Clean up MCP registrations for sessions that no longer exist
	channel.CleanupStaleMCP(func(sessionID string) bool {
		return !isOwnSessionID(hostname, sessionID) || sessionExists(sessionID)
	})
}

// isOwnSessionID reports whether sessionID is named as a session of the daemon
// on hostname: "<hostname>-<id>", where the id holds no hyphen. claude's MCP
// list is shared by every daemon that runs as this user, and a daemon cannot
// tell whether another one's session still exists.
func isOwnSessionID(hostname, sessionID string) bool {
	id, ok := strings.CutPrefix(sessionID, hostname+"-")
	return ok && id != "" && !strings.Contains(id, "-")
}
