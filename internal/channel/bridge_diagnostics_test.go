package channel

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestBridgeHealth_LiveBridge(t *testing.T) {
	// A stand-in for the real bridge: a loopback server answering /health.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	_, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	alive, reason := BridgeHealth(port)
	if !alive {
		t.Errorf("alive = false, reason = %q; want live bridge to be alive", reason)
	}
	if reason != "" {
		t.Errorf("reason = %q for live bridge, want empty", reason)
	}
}

func TestBridgeHealth_BridgeExitedReason(t *testing.T) {
	// Port with nothing listening — the incident's permanent drop class:
	// the bridge process exited and nothing respawns it. The close reason
	// must say that plainly.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close() // free the port so nothing is listening
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	alive, reason := BridgeHealth(port)
	if alive {
		t.Fatalf("alive = true for a dead bridge port")
	}
	if !strings.Contains(reason, "not listening") && !strings.Contains(reason, "exited") {
		t.Errorf("reason = %q; want it to name the bridge exit / no listener", reason)
	}
}

func TestBridgeHealth_NoPort(t *testing.T) {
	alive, reason := BridgeHealth(0)
	if alive {
		t.Fatalf("alive = true with no registered port")
	}
	if !strings.Contains(reason, "no channel port registered") {
		t.Errorf("reason = %q; want no-port diagnostic", reason)
	}
}

func TestBridgeHealth_WedgedBridgeTimeout(t *testing.T) {
	// A listener that accepts connections but never answers: a wedged
	// bridge. Must be classified as a timeout, not a dial failure.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	var port int
	_, _ = fmt.Sscanf(mustPort(l), "%d", &port)

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c // accept and hold, never answer
		}
	}()

	old := ProbeTimeout
	ProbeTimeout = 300 * time.Millisecond
	defer func() { ProbeTimeout = old }()

	alive, reason := BridgeHealth(port)
	if alive {
		t.Fatalf("alive = true for a wedged bridge")
	}
	if !strings.Contains(reason, "wedged") {
		t.Errorf("reason = %q; want wedged/timeout diagnostic", reason)
	}
}

func mustPort(l net.Listener) string {
	_, s, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		panic(err)
	}
	return s
}

func TestBuildSelfScope_ReportsBridgeLiveness(t *testing.T) {
	// A healthy bridge → bridge_alive=true, no close reason.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer ts.Close()
	livePort := 0
	_, _ = fmt.Sscanf(mustPort(ts.Listener), "%d", &livePort)

	// A dead bridge (recorded port, nothing listening) → bridge_alive=false
	// with the close-reason diagnostic attached.
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	deadPort := 0
	_, _ = fmt.Sscanf(mustPort(l2), "%d", &deadPort)
	_ = l2.Close() // free the port so nothing is listening
	sess := &session.Session{
		ID: "aa01", FullID: "h1-aa01", Hostname: "h1",
		State: session.StateRunning, ChannelReady: true,
	}

	live := *sess
	live.ChannelPort = livePort
	sc := BuildSelfScope(&live, nil)
	if !sc.BridgeAlive || sc.BridgeCloseReason != "" {
		t.Errorf("live bridge: BridgeAlive=%v reason=%q, want true/empty", sc.BridgeAlive, sc.BridgeCloseReason)
	}

	dead := *sess
	dead.ChannelPort = deadPort
	sc = BuildSelfScope(&dead, nil)
	if sc.BridgeAlive {
		t.Errorf("dead bridge: BridgeAlive=true")
	}
	if sc.BridgeCloseReason == "" {
		t.Errorf("dead bridge: BridgeCloseReason empty; want the close-reason diagnostic")
	}

	// Peers of a session that has no port at all → no spurious reason noise.
	peer := &session.Session{ID: "bb01", FullID: "h1-bb01", Hostname: "h1", State: session.StateRunning}
	sc = BuildSelfScope(sess, []*session.Session{peer})
	if len(sc.Peers) != 1 || sc.Peers[0].MCPServerName != "datawatch-h1-bb01" {
		t.Errorf("peers wrong: %+v", sc.Peers)
	}
}
