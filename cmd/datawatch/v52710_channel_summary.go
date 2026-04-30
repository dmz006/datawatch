// v5.27.10 (BL216) — channelInfoSummary renders the daemon's
// resolved bridge state as a chat-friendly multi-line string, used by
// router.SetChannelInfoFn. Reads channel package state directly so it
// works without an HTTP round-trip.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/channel"
)

func channelInfoSummary() string {
	dataDir := os.Getenv("DATAWATCH_DATA_DIR")
	if dataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, ".datawatch")
		}
	}

	kind := channel.BridgeKind()
	path := channel.BridgePath()
	probe := channel.Probe(dataDir)

	var b strings.Builder
	fmt.Fprintf(&b, "channel: %s bridge", kind)
	if path != "" {
		fmt.Fprintf(&b, " (%s)", path)
	}
	fmt.Fprintf(&b, ", ready=%v", probe.Ready)
	if probe.Hint != "" {
		fmt.Fprintf(&b, " — %s", probe.Hint)
	}

	// Mirror the /api/channel/info stale check.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeMCP := filepath.Join(home, ".mcp.json")
		if stale, missing, err := channel.IsStaleProjectMCPConfig(homeMCP); err == nil && stale {
			fmt.Fprintf(&b, "\nstale: %s → %s (run `datawatch channel cleanup-stale-mcp-json`)",
				homeMCP, missing)
		}
	}

	return b.String()
}
