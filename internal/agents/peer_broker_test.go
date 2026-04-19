// F10 sprint 7 (S7.6) — peer broker tests.

package agents

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// peerFixture wires a Manager with two agents — one whose profile
// allows P2P, one that doesn't — plus a broker bound to it.
func peerFixture(t *testing.T) (*PeerBroker, *Manager, string, string) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:               "talker",
		Git:                profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair:          profile.ImagePair{Agent: "agent-claude"},
		Memory:             profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowPeerMessaging: true,
	})
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "listener",
		Git:       profile.GitSpec{URL: "https://github.com/x/y2"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		// AllowPeerMessaging: false (default) — receive-only is OK
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	m.RegisterDriver(&fakeDriver{kind: "docker"})

	talker, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "talker", ClusterProfile: "c", Branch: "t"})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "listener", ClusterProfile: "c", Branch: "l"})
	if err != nil {
		t.Fatal(err)
	}
	return NewPeerBroker(m, 0), m, talker.ID, listener.ID
}

func TestPeerBroker_HappyPath(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	delivered, dropped, err := b.Send(sender, []string{recip}, "hello", "first message")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if delivered != 1 || len(dropped) != 0 {
		t.Errorf("delivered=%d dropped=%v want 1/empty", delivered, dropped)
	}
	if got := b.InboxLen(recip); got != 1 {
		t.Errorf("inbox len=%d want 1", got)
	}
	msgs := b.Drain(recip)
	if len(msgs) != 1 || msgs[0].From != sender || msgs[0].Body != "first message" {
		t.Errorf("drained mismatch: %+v", msgs)
	}
	if b.InboxLen(recip) != 0 {
		t.Error("Drain should empty the inbox")
	}
}

func TestPeerBroker_SenderProfileNotAllowed(t *testing.T) {
	b, _, _, recip := peerFixture(t)
	// Use the listener (AllowPeerMessaging=false) as the sender —
	// the profile snapshot was captured at spawn time so re-mutating
	// the store after spawn doesn't retroactively demote a running
	// agent (deliberate: operators shouldn't yank P2P out from
	// under a worker that was launched with it).
	_, _, err := b.Send(recip, []string{recip}, "x", "y")
	if err == nil {
		t.Fatal("expected denial when sender profile lacks AllowPeerMessaging")
	}
	if !strings.Contains(err.Error(), "allow_peer_messaging") {
		t.Errorf("error wording: %v", err)
	}
}

func TestPeerBroker_UnknownRecipientDropped(t *testing.T) {
	b, _, sender, _ := peerFixture(t)
	delivered, dropped, err := b.Send(sender, []string{"ghost"}, "x", "y")
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 0 || len(dropped) != 1 || dropped[0] != "ghost" {
		t.Errorf("delivered=%d dropped=%v want 0/[ghost]", delivered, dropped)
	}
}

func TestPeerBroker_TerminalRecipientDropped(t *testing.T) {
	b, m, sender, recip := peerFixture(t)
	if err := m.Terminate(context.Background(), recip); err != nil {
		t.Fatal(err)
	}
	_, dropped, _ := b.Send(sender, []string{recip}, "x", "y")
	if len(dropped) != 1 {
		t.Errorf("Stopped recipient should be dropped: %v", dropped)
	}
}

func TestPeerBroker_FanoutPartialDrop(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	delivered, dropped, _ := b.Send(sender, []string{recip, "ghost"}, "x", "y")
	if delivered != 1 || len(dropped) != 1 {
		t.Errorf("partial fanout: delivered=%d dropped=%v", delivered, dropped)
	}
}

func TestPeerBroker_InboxCapEnforced(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	b.maxInbox = 2
	for i := 0; i < 2; i++ {
		if d, _, _ := b.Send(sender, []string{recip}, "x", "msg"); d != 1 {
			t.Fatalf("seed message %d not delivered", i)
		}
	}
	// Third one should be dropped — inbox full.
	d, dropped, _ := b.Send(sender, []string{recip}, "x", "overflow")
	if d != 0 || len(dropped) != 1 {
		t.Errorf("over-cap: delivered=%d dropped=%v", d, dropped)
	}
}

func TestPeerBroker_PeekDoesNotConsume(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	_, _, _ = b.Send(sender, []string{recip}, "x", "first")
	_, _, _ = b.Send(sender, []string{recip}, "x", "second")
	snap := b.Peek(recip)
	if len(snap) != 2 {
		t.Errorf("peek len=%d want 2", len(snap))
	}
	if b.InboxLen(recip) != 2 {
		t.Error("Peek should not consume")
	}
}

// Concurrent sends from many goroutines: no race + no lost messages.
func TestPeerBroker_ConcurrentSendsSafe(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	b.maxInbox = 1000
	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = b.Send(sender, []string{recip}, "x", "concurrent")
		}()
	}
	wg.Wait()
	if got := b.InboxLen(recip); got != N {
		t.Errorf("concurrent: inbox=%d want %d", got, N)
	}
}

// Validation paths.
func TestPeerBroker_RejectsEmptyArgs(t *testing.T) {
	b, _, sender, recip := peerFixture(t)
	if _, _, err := b.Send("", []string{recip}, "x", "y"); err == nil {
		t.Error("empty sender should fail")
	}
	if _, _, err := b.Send(sender, nil, "x", "y"); err == nil {
		t.Error("empty recipient list should fail")
	}
}
