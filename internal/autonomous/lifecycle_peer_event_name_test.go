// Tests for the peer name on a forwarded lifecycle event: the receiver sets it
// from the authenticated peer identity, never from the payload; local events
// keep an empty peer; a filter can select by peer.

package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// peerEventNameRecv delivers one forwarded event through m as peer and returns
// what a local subscriber saw.
func peerEventNameRecv(t *testing.T, m *Manager, peer string, ev LifecycleEvent) LifecycleEvent {
	t.Helper()
	got := make(chan LifecycleEvent, 1)
	defer m.Subscribe(LifecycleFilter{}, func(e LifecycleEvent) { got <- e })()
	if err := m.ReceiveForwardedEventFrom(peer, recvEnvelope(ev, originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEventFrom: %v", err)
	}
	select {
	case out := <-got:
		return out
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive the forwarded event")
	}
	return LifecycleEvent{}
}

func TestPeerEventNameSetFromAuthenticatedPeer(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	out := peerEventNameRecv(t, m, "peer-alpha", recvPRDEvent())
	if out.Peer != "peer-alpha" {
		t.Fatalf("Peer = %q, want peer-alpha", out.Peer)
	}
	if out.Host != "daemon-a" || out.Forward == nil {
		t.Fatalf("the origin host and forward chain changed: %+v", out)
	}
}

func TestPeerEventNameIgnoresPayloadValue(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	ev := recvPRDEvent()
	ev.Peer = "spoofed-peer"

	// A peer that authenticated as peer-alpha cannot claim another name.
	if out := peerEventNameRecv(t, m, "peer-alpha", ev); out.Peer != "peer-alpha" {
		t.Fatalf("Peer = %q, want the authenticated name peer-alpha", out.Peer)
	}
	// With no authenticated peer the payload value is dropped, not kept.
	if out := peerEventNameRecv(t, m, "", ev); out.Peer != "" {
		t.Fatalf("Peer = %q, want empty when no peer authenticated", out.Peer)
	}
	// The one-argument entry point is the no-peer path.
	got := make(chan LifecycleEvent, 1)
	defer m.Subscribe(LifecycleFilter{}, func(e LifecycleEvent) { got <- e })()
	if err := m.ReceiveForwardedEvent(recvEnvelope(ev, originChain())); err != nil {
		t.Fatalf("ReceiveForwardedEvent: %v", err)
	}
	select {
	case out := <-got:
		if out.Peer != "" {
			t.Fatalf("Peer = %q, want empty", out.Peer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery")
	}
}

func TestPeerEventNameLocalEventHasNoPeer(t *testing.T) {
	m := new(Manager)
	got := make(chan LifecycleEvent, 1)
	defer m.Subscribe(LifecycleFilter{}, func(e LifecycleEvent) { got <- e })()
	m.emit(recvPRDEvent())
	select {
	case out := <-got:
		if out.Peer != "" {
			t.Fatalf("a local event carries Peer %q, want empty", out.Peer)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), `"peer"`) {
			t.Fatalf("a local event serialises a peer key: %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery")
	}
}

func TestPeerEventNameFilterSelectsByPeer(t *testing.T) {
	forwarded := recvPRDEvent()
	forwarded.Peer = "peer-alpha"
	local := recvPRDEvent()

	cases := []struct {
		name   string
		filter LifecycleFilter
		ev     LifecycleEvent
		want   bool
	}{
		{"empty filter passes a forwarded event", LifecycleFilter{}, forwarded, true},
		{"empty filter passes a local event", LifecycleFilter{}, local, true},
		{"matching peer", LifecycleFilter{Peer: "peer-alpha"}, forwarded, true},
		{"other peer", LifecycleFilter{Peer: "peer-beta"}, forwarded, false},
		{"peer filter refuses a local event", LifecycleFilter{Peer: "peer-alpha"}, local, false},
		{"peer and kind both match", LifecycleFilter{Peer: "peer-alpha", Kinds: []string{PRDStatusKind(PRDCompleted)}}, forwarded, true},
		{"peer matches but kind does not", LifecycleFilter{Peer: "peer-alpha", Kinds: []string{PRDStatusKind(PRDFailed)}}, forwarded, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.filter.Matches(c.ev); got != c.want {
				t.Fatalf("Matches = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPeerEventNameFilterReachesSubscriber(t *testing.T) {
	m := recvManager(recvEnabled(), recvLocal)
	alpha := make(chan LifecycleEvent, 4)
	beta := make(chan LifecycleEvent, 4)
	defer m.Subscribe(LifecycleFilter{Peer: "peer-alpha"}, func(e LifecycleEvent) { alpha <- e })()
	defer m.Subscribe(LifecycleFilter{Peer: "peer-beta"}, func(e LifecycleEvent) { beta <- e })()

	if err := m.ReceiveForwardedEventFrom("peer-alpha", recvEnvelope(recvPRDEvent(), originChain())); err != nil {
		t.Fatalf("receive: %v", err)
	}
	select {
	case e := <-alpha:
		if e.Peer != "peer-alpha" {
			t.Fatalf("Peer = %q", e.Peer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the peer-alpha subscription did not receive the event")
	}
	select {
	case e := <-beta:
		t.Fatalf("the peer-beta subscription received an event from %q", e.Peer)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestPeerEventNameSubscriptionValidation(t *testing.T) {
	base := Subscription{
		Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}},
		Sink:   SubscriptionSink{Type: SinkMCPQueue, Target: "peer-events"},
	}
	ok := base
	ok.Filter.Peer = "peer-alpha"
	if err := ValidateSubscription(ok, time.Now()); err != nil {
		t.Fatalf("a registered-name peer filter was refused: %v", err)
	}
	for _, bad := range []string{"has space", "semi;colon", strings.Repeat("a", MaxPeerNameLen+1)} {
		sub := base
		sub.Filter.Peer = bad
		if err := ValidateSubscription(sub, time.Now()); err == nil {
			t.Fatalf("peer filter %q was accepted", bad)
		}
	}
}

func TestPeerEventNameDoesNotLeaveThisHost(t *testing.T) {
	// A received event carries its peer name but is still never forwarded on.
	ev := recvPRDEvent()
	ev.Peer = "peer-alpha"
	chain := originChain()
	ev.Forward = &chain
	if reason := forwardRefusal(ev.Forward, recvLocal, DefaultForwardMaxHops); reason == "" {
		t.Fatal("a forwarded event with a peer name may be forwarded on")
	}
}
