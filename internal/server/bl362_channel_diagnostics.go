// BL362 — channel bridge diagnostics endpoint.
//
// Exposes per-session and global channel-bridge diagnostic data:
// bridge kind/path, per-session listening port, daemon-side readiness,
// and a live probe of each session's bridge /health endpoint.
// Complements the startup stderr diagnostics added to datawatch-channel
// (port-conflict identification, pre-flight daemon probe) with a
// queryable surface operators can call without grepping logs.

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/federation"
)

// ChannelDiagnostics is the on-the-wire shape of GET /api/channel/diagnostics.
type ChannelDiagnostics struct {
	// BridgeKind: "go" or "js" — which bridge the daemon resolved.
	BridgeKind string `json:"bridge_kind"`
	// BridgePath: resolved binary/script path.
	BridgePath string `json:"bridge_path"`
	// GlobalPort: server.channel_port config value (0 = auto/random).
	GlobalPort int `json:"global_port"`
	// Sessions: per-session bridge status.
	Sessions []ChannelSessionDiag `json:"sessions"`
	// Hints: actionable remediation hints when problems are detected.
	Hints []string `json:"hints,omitempty"`
}

// ChannelSessionDiag is the per-session slice of ChannelDiagnostics.
type ChannelSessionDiag struct {
	// SessionID: short session hex ID.
	SessionID string `json:"session_id"`
	// SessionName: human name if set.
	SessionName string `json:"session_name,omitempty"`
	// ChannelPort: port the bridge registered via POST /api/channel/ready.
	// 0 means the bridge has not called ready yet.
	ChannelPort int `json:"channel_port"`
	// BridgeAlive: result of a live GET http://127.0.0.1:PORT/health probe.
	// false when ChannelPort==0 or the probe fails.
	BridgeAlive bool `json:"bridge_alive"`
	// ProbeError: human-readable error when BridgeAlive is false.
	ProbeError string `json:"probe_error,omitempty"`
}

// handleChannelDiagnostics serves GET /api/channel/diagnostics.
func (s *Server) handleChannelDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	diag := ChannelDiagnostics{
		BridgeKind: channel.BridgeKind(),
		BridgePath: channel.BridgePath(),
		Sessions:   []ChannelSessionDiag{},
	}
	if s.cfg != nil {
		diag.GlobalPort = s.cfg.Server.ChannelPort
	}

	// Walk all sessions and probe their bridges.
	if s.manager != nil {
		for _, sess := range s.manager.ListSessions() {
			sd := ChannelSessionDiag{
				SessionID:   sess.ID,
				SessionName: sess.Name,
				ChannelPort: sess.ChannelPort,
			}
			if sess.ChannelPort > 0 {
				if err := probeChannelBridge(sess.ChannelPort); err != nil {
					sd.BridgeAlive = false
					sd.ProbeError = err.Error()
				} else {
					sd.BridgeAlive = true
				}
			} else {
				sd.ProbeError = "bridge has not called ready yet (ChannelPort=0)"
			}
			diag.Sessions = append(diag.Sessions, sd)
		}
	}

	// Append actionable hints for common problems.
	for _, sd := range diag.Sessions {
		if !sd.BridgeAlive && sd.ChannelPort == 0 {
			diag.Hints = append(diag.Hints,
				fmt.Sprintf("session %s: bridge never sent /ready — check DATAWATCH_API_URL and token in the session's environment", sd.SessionID))
		} else if !sd.BridgeAlive {
			diag.Hints = append(diag.Hints,
				fmt.Sprintf("session %s: bridge on port %d did not respond to /health — it may have crashed; try restarting the session", sd.SessionID, sd.ChannelPort))
		}
	}
	if diag.BridgeKind == "js" {
		diag.Hints = append(diag.Hints, "bridge_kind=js: Node.js fallback is active; install datawatch-channel binary for the Go bridge (`datawatch setup channel`)")
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(diag)
}

// probeChannelBridge does a GET http://127.0.0.1:PORT/health with a short
// timeout to verify the bridge process is alive.
func probeChannelBridge(port int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
