// Test helpers for hub client registration.
//
// Hub.register is an unbuffered channel consumed by Hub.Run, which only
// sets h.clients[c] after receiving the send — so immediately after
// 'h.register <- c' returns, ClientCount() can still be 0. Tests that
// broadcast right after registering (or a server that early-returns
// when ClientCount()==0) see the broadcast dropped under scheduler
// pressure. These helpers make registration wait until the client is
// actually held by the hub.

package server

import (
	"testing"
	"time"
)

// registerHubTimeout bounds waiting for Hub.Run to process a register
// (or unregister) send under scheduler pressure.
const registerHubTimeout = wsTestFrameTimeout

// waitForHubClients polls h.ClientCount() every 5ms until it is >=
// want, bounded by registerHubTimeout.
func waitForHubClients(t *testing.T, h *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(registerHubTimeout)
	for {
		if got := h.ClientCount(); got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for the hub to hold %d client(s) after register; it holds %d", registerHubTimeout, want, h.ClientCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// registerTestClient sends c on h.register (with a bounded select so a
// missing 'go hub.Run()' fails with a readable message instead of
// hanging) and then waits until the hub actually holds it.
func registerTestClient(t *testing.T, h *Hub, c *client) {
	t.Helper()
	before := h.ClientCount()
	select {
	case h.register <- c:
	case <-time.After(registerHubTimeout):
		t.Fatalf("timed out (%s) sending the test client on h.register; is 'go h.Run()' running?", registerHubTimeout)
	}
	waitForHubClients(t, h, before+1)
}
