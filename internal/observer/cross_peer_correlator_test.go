// BL180 Phase 2 cross-host (v5.12.0) — federation-aware caller
// attribution: a session on host A talking to ollama on host B
// produces a Caller entry on host B's ollama envelope with the
// `<peer>:<envelope-id>` prefix.

package observer

import (
	"testing"
)

func TestCorrelateAcrossPeers_HappyPath(t *testing.T) {
	// Host A: a session envelope with one outbound edge to host B's
	// ollama port (11434).
	hostA := []Envelope{{
		ID:    "session:opencode-x1y2",
		Kind:  EnvelopeSession,
		Label: "opencode",
		OutboundEdges: []OutboundEdge{
			{TargetIP: "10.0.0.7", TargetPort: 11434, PID: 1234, Conns: 3},
		},
	}}
	// Host B: an ollama backend envelope listening on 10.0.0.7:11434.
	hostB := []Envelope{{
		ID:    "backend:ollama",
		Kind:  EnvelopeBackend,
		Label: "ollama",
		ListenAddrs: []ListenAddr{
			{IP: "10.0.0.7", Port: 11434},
		},
	}}

	byPeer := map[string][]Envelope{
		"workstation-2":   hostA,
		"k8s-cluster-prod": hostB,
	}
	CorrelateAcrossPeers(byPeer, "primary")

	bk := byPeer["k8s-cluster-prod"][0]
	if len(bk.Callers) != 1 {
		t.Fatalf("backend Callers count = %d, want 1: %+v", len(bk.Callers), bk.Callers)
	}
	want := "workstation-2:session:opencode-x1y2"
	if bk.Callers[0].Caller != want {
		t.Fatalf("caller = %q, want %q", bk.Callers[0].Caller, want)
	}
	if bk.Callers[0].Conns != 3 {
		t.Fatalf("conns = %d, want 3", bk.Callers[0].Conns)
	}
	if bk.Caller != want {
		t.Fatalf("loudest alias not set: %q", bk.Caller)
	}
	if bk.CallerKind != "session" {
		t.Fatalf("CallerKind = %q, want session", bk.CallerKind)
	}
}

func TestCorrelateAcrossPeers_WildcardListener(t *testing.T) {
	hostA := []Envelope{{
		ID:   "session:s1",
		Kind: EnvelopeSession,
		OutboundEdges: []OutboundEdge{
			{TargetIP: "10.0.0.42", TargetPort: 8080, PID: 1, Conns: 1},
		},
	}}
	// Listener on 0.0.0.0:8080 — should still match.
	hostB := []Envelope{{
		ID:   "backend:web",
		Kind: EnvelopeBackend,
		ListenAddrs: []ListenAddr{
			{IP: "0.0.0.0", Port: 8080},
		},
	}}
	byPeer := map[string][]Envelope{
		"a": hostA,
		"b": hostB,
	}
	CorrelateAcrossPeers(byPeer, "primary")
	if len(byPeer["b"][0].Callers) != 1 {
		t.Fatalf("wildcard match failed: %+v", byPeer["b"][0])
	}
}

func TestCorrelateAcrossPeers_SamePeerNotMatched(t *testing.T) {
	// Edge and listener on the same peer — must NOT generate a
	// cross-peer attribution (that's the local correlator's job).
	host := []Envelope{
		{
			ID:   "session:s1",
			Kind: EnvelopeSession,
			OutboundEdges: []OutboundEdge{
				{TargetIP: "127.0.0.1", TargetPort: 11434, Conns: 5},
			},
		},
		{
			ID:   "backend:ollama",
			Kind: EnvelopeBackend,
			ListenAddrs: []ListenAddr{
				{IP: "127.0.0.1", Port: 11434},
			},
		},
	}
	byPeer := map[string][]Envelope{"a": host, "b": {}}
	CorrelateAcrossPeers(byPeer, "primary")
	if len(byPeer["a"][1].Callers) != 0 {
		t.Fatalf("same-peer pair should not cross-attribute: %+v", byPeer["a"][1].Callers)
	}
}

func TestCorrelateAcrossPeers_NoOpWithSinglePeer(t *testing.T) {
	envs := []Envelope{{
		ID:   "session:s1",
		Kind: EnvelopeSession,
		OutboundEdges: []OutboundEdge{
			{TargetIP: "10.0.0.7", TargetPort: 11434, Conns: 1},
		},
	}}
	byPeer := map[string][]Envelope{"only-peer": envs}
	CorrelateAcrossPeers(byPeer, "only-peer")
	if len(byPeer["only-peer"][0].Callers) != 0 {
		t.Fatalf("single-peer call must be a no-op")
	}
}

func TestCorrelateAcrossPeers_CallersSortedByConnsDesc(t *testing.T) {
	hostA := []Envelope{
		{
			ID:   "session:loud",
			Kind: EnvelopeSession,
			OutboundEdges: []OutboundEdge{
				{TargetIP: "10.0.0.1", TargetPort: 80, Conns: 10},
			},
		},
		{
			ID:   "session:quiet",
			Kind: EnvelopeSession,
			OutboundEdges: []OutboundEdge{
				{TargetIP: "10.0.0.1", TargetPort: 80, Conns: 1},
			},
		},
	}
	hostB := []Envelope{{
		ID:   "backend:web",
		Kind: EnvelopeBackend,
		ListenAddrs: []ListenAddr{
			{IP: "10.0.0.1", Port: 80},
		},
	}}
	byPeer := map[string][]Envelope{"a": hostA, "b": hostB}
	CorrelateAcrossPeers(byPeer, "primary")

	cs := byPeer["b"][0].Callers
	if len(cs) != 2 {
		t.Fatalf("Callers count = %d, want 2", len(cs))
	}
	if cs[0].Conns < cs[1].Conns {
		t.Fatalf("Callers not sorted desc by Conns: %+v", cs)
	}
	if cs[0].Caller != "a:session:loud" {
		t.Fatalf("loudest = %q, want a:session:loud", cs[0].Caller)
	}
}

func TestCorrelateAcrossPeers_OutboundEdgeWithoutListenerDropped(t *testing.T) {
	// Outbound edge to an IP/port that no peer listens on — leaves the
	// session envelope untouched on either side.
	hostA := []Envelope{{
		ID:   "session:s1",
		Kind: EnvelopeSession,
		OutboundEdges: []OutboundEdge{
			{TargetIP: "8.8.8.8", TargetPort: 53, Conns: 2}, // public DNS
		},
	}}
	hostB := []Envelope{{
		ID:   "backend:web",
		Kind: EnvelopeBackend,
		ListenAddrs: []ListenAddr{
			{IP: "10.0.0.1", Port: 80},
		},
	}}
	byPeer := map[string][]Envelope{"a": hostA, "b": hostB}
	CorrelateAcrossPeers(byPeer, "primary")
	if len(byPeer["b"][0].Callers) != 0 {
		t.Fatalf("unrelated outbound edge generated attribution: %+v", byPeer["b"][0].Callers)
	}
}

func TestCorrelateAcrossPeers_LocalPeerNameSuppressesPrefix(t *testing.T) {
	// When the client peer matches localPeerName, don't prefix the
	// caller ID — keeps single-host renders clean for the primary's
	// own envelopes.
	hostLocal := []Envelope{{
		ID:   "session:local-s1",
		Kind: EnvelopeSession,
		OutboundEdges: []OutboundEdge{
			{TargetIP: "10.0.0.7", TargetPort: 11434, Conns: 1},
		},
	}}
	hostB := []Envelope{{
		ID:   "backend:ollama",
		Kind: EnvelopeBackend,
		ListenAddrs: []ListenAddr{
			{IP: "10.0.0.7", Port: 11434},
		},
	}}
	byPeer := map[string][]Envelope{"local": hostLocal, "remote-b": hostB}
	CorrelateAcrossPeers(byPeer, "local")
	got := byPeer["remote-b"][0].Callers[0].Caller
	if got != "session:local-s1" {
		t.Fatalf("local-peer caller should not be prefixed: got %q", got)
	}
}
