// Bridge liveness + close-reason diagnostics for per-session MCP channel
// bridges.
//
// The 2026-10-15 mid-session CONNECTION_CLOSED incident (BL463,
// docs/plans/2026-10-20-bl463-mcp-drop-findings.md) left one gap: when a
// per-session bridge (the process a session's client talks to over stdio,
// which itself serves /health + /send on its own loopback HTTP endpoint)
// dies, the daemon keeps the stale ChannelReady/ChannelPort on the
// session record and the session itself has no way to ask "is my bridge
// actually still up, and if not, why?". The bridge side already re-announces
// itself to a restarted daemon (readyHeartbeat, GH#174); the daemon side
// needed the inverse — a liveness answer with a reason, surfaced where a
// session already asks about itself (get_my_scope and
// GET /api/sessions/self/scope, both built via channel.BuildSelfScope).
//
// The reason strings are the diagnosability contract: a bridge that exited
// (nothing listening — no process respawns it, a new session is required)
// is recorded distinctly from a wedged bridge (a listener is up but does
// not answer /health) and from an unhealthy HTTP answer, matching the
// "connection close events are logged with a reason" bar for this fix.

package channel

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// ProbeTimeout is the /health probe deadline. Loopback probes resolve in
// sub-millisecond for a live or refused bridge; only a genuinely wedged
// bridge approaches the deadline. Overridable in tests.
var ProbeTimeout = 2 * time.Second

// BridgeHealth probes the bridge's own /health endpoint on 127.0.0.1:<port>
// and reports whether the per-session channel bridge is alive. The returned
// reason is a close-event diagnostic: it says WHAT is wrong so a drop is
// diagnosable without server logs.
//
//   - port <= 0:  no channel port registered (bridge never announced via /api/channel/ready)
//   - dial error: no process listening — the bridge exited and is not
//     automatically respawned; the session needs a restart
//   - timeout:    a listener is up but did not answer in time (wedged)
//   - HTTP >=400: the bridge answered but reported failure
//
// A healthy bridge returns (true, "").
func BridgeHealth(port int) (alive bool, reason string) {
	if port <= 0 {
		return false, "no channel port registered (bridge never announced via /api/channel/ready)"
	}
	client := &http.Client{Timeout: ProbeTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port)) // #nosec G112 -- loopback-only bridge endpoint; port comes from a fixed session record
	if err != nil {
		return false, bridgeProbeReason(port, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 400 {
		return false, fmt.Sprintf("bridge on port %d answered /health with HTTP %d (unhealthy)", port, resp.StatusCode)
	}
	return true, ""
}

// bridgeProbeReason classifies a failed /health probe into the
// close-reason string a session-facing surface should report.
func bridgeProbeReason(port int, err error) string {
	// Any deadline-style failure (dial or read) means a listener was
	// accepted but the bridge never answered — a wedged bridge.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("bridge on port %d did not answer /health within %s (wedged)", port, ProbeTimeout)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return fmt.Sprintf("no process is listening on port %d — this session's bridge has exited (%s); bridges are not respawned automatically, restart the session to re-establish the MCP transport", port, err)
	}
	return fmt.Sprintf("bridge on port %d failed its health probe: %v", port, err)
}
