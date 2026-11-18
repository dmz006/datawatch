// The receive endpoint names the sending peer on the event from the
// authenticated peer identity, never from the payload.

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

func peerEventNameWait(t *testing.T, ch <-chan autonomous.LifecycleEvent) autonomous.LifecycleEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("subscriber did not receive the forwarded event")
	}
	return autonomous.LifecycleEvent{}
}

func TestPeerEventNameFromAuthenticatedPeerNotPayload(t *testing.T) {
	s, mgr, _, _ := fwdReceiver(t, "daemon-b", true)
	got := make(chan autonomous.LifecycleEvent, 2)
	defer mgr.Subscribe(autonomous.LifecycleFilter{}, func(ev autonomous.LifecycleEvent) { got <- ev })()

	peer := fwdPeer("peer-alpha", fwdPeerToken, federation.CapAutonomousWrite)
	ev := fwdEvent("ev-peer-name-1")
	ev.Peer = "spoofed-peer"
	rr := fwdSigned(t, s, fwdBody(t, ev, fwdChain(), "daemon-a"), peer)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rr.Code, rr.Body.String())
	}
	if out := peerEventNameWait(t, got); out.Peer != "peer-alpha" {
		t.Fatalf("Peer = %q, want the authenticated peer-alpha, not the payload value", out.Peer)
	}
}

func TestPeerEventNameEmptyForAdminCaller(t *testing.T) {
	s, mgr, _, _ := fwdReceiver(t, "daemon-b", true)
	got := make(chan autonomous.LifecycleEvent, 2)
	defer mgr.Subscribe(autonomous.LifecycleFilter{}, func(ev autonomous.LifecycleEvent) { got <- ev })()

	ev := fwdEvent("ev-peer-name-2")
	ev.Peer = "spoofed-peer"
	rr := fwdPost(t, s, fwdBody(t, ev, fwdChain(), "daemon-a"), nil, "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rr.Code, rr.Body.String())
	}
	if out := peerEventNameWait(t, got); out.Peer != "" {
		t.Fatalf("Peer = %q, want empty for a caller that is not a registered peer", out.Peer)
	}
}
