// BL362 — channelDiagnosticsSummary renders per-session bridge diagnostic
// data as a chat-friendly string for router.SetChannelDiagnosticsFn.
// Uses the HTTP API so it gets the live daemon state including per-session
// ChannelPort and /health probe results.

package main

import (
	"fmt"
	"strings"
)

func channelDiagnosticsSummary() string {
	diag, err := fetchChannelDiagnostics()
	if err != nil {
		return fmt.Sprintf("channel diagnostics: error fetching from daemon: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "channel diagnostics: bridge=%s", diag.BridgeKind)
	if diag.BridgePath != "" {
		fmt.Fprintf(&b, " (%s)", diag.BridgePath)
	}
	if diag.GlobalPort > 0 {
		fmt.Fprintf(&b, " global_port=%d", diag.GlobalPort)
	}

	if len(diag.Sessions) == 0 {
		fmt.Fprintf(&b, "\n  (no sessions)")
	}
	for _, sd := range diag.Sessions {
		name := sd.SessionName
		if name == "" {
			name = sd.SessionID
		}
		if sd.BridgeAlive {
			fmt.Fprintf(&b, "\n  %s port=%d bridge=alive", name, sd.ChannelPort)
		} else {
			errStr := sd.ProbeError
			if errStr == "" {
				errStr = "not ready"
			}
			fmt.Fprintf(&b, "\n  %s port=%d bridge=DEAD (%s)", name, sd.ChannelPort, errStr)
		}
	}
	for _, h := range diag.Hints {
		fmt.Fprintf(&b, "\n  hint: %s", h)
	}
	return b.String()
}
