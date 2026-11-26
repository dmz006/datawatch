package channel

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Time limits for the claude calls of the startup cleanup.
var (
	mcpListTimeout   = 2 * time.Minute
	mcpRemoveTimeout = 30 * time.Second
)

// SetRegistrationRecord names the data folder that records the registrations
// this daemon makes. Not recorded yet.
func SetRegistrationRecord(dataDir string) {}

// CleanupStaleRegistrations runs the startup cleanup of claude's MCP list.
func CleanupStaleRegistrations(_ context.Context, usingGoBridge bool, sessionExists func(string) bool) {
	if usingGoBridge {
		if cleaned := CleanupStaleJSRegistrations(); len(cleaned) > 0 {
			fmt.Printf("[channel] removed stale JS-bridge MCP registration(s): %s\n", strings.Join(cleaned, ", "))
		}
	}
	UnregisterGlobalMCP()
	CleanupStaleMCP(sessionExists)
}
