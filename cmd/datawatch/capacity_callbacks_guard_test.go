package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/session"
)

// These tests guard the lock rule on capacity.Options: every callback runs
// under the ledger lock, so none may block on a slow peer or call back into
// the ledger. They build the ledger from the same callback constructors
// wireCapacity uses.

const guardDeadline = time.Second

// guardHash is a valid-looking fingerprint hash for the test match.
const guardHash = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

// completesWithin runs fn in a goroutine and reports whether it returned
// before d.
func completesWithin(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// mustComplete fails the test with a goroutine dump when fn does not return
// within guardDeadline.
func mustComplete(t *testing.T, name string, fn func()) {
	t.Helper()
	if !completesWithin(guardDeadline, fn) {
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		t.Fatalf("%s did not complete within %s: the ledger is blocked\n%s", name, guardDeadline, buf)
	}
}

// guardLister is a session lister with a fixed hostname and no sessions.
type guardLister struct{ sessions []*session.Session }

func (guardLister) Hostname() string                   { return "guard-host" }
func (g guardLister) ListSessions() []*session.Session { return g.sessions }

// slowPeerGate wires fingerprintPeerGate to a gate holding a CONFIRMED node
// match "gpu-0" with an opted-in peer whose /api/capacity handler is slow.
// It returns a counter of requests the peer received.
func slowPeerGate(t *testing.T) *int32 {
	t.Helper()
	hits := new(int32)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	matches, err := fingerprint.NewMatchStore("")
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	m, _ := matches.Observe("peer-a", fingerprint.KindNode, "gpu-0", "peer-node", guardHash)
	if _, err := matches.Confirm(m.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	peers := func() []fingerprint.PeerRef {
		return []fingerprint.PeerRef{{Name: "peer-a", URL: srv.URL, Token: "tok", OptedIn: true}}
	}
	gate := &fingerprint.PeerGate{
		Matches: matches,
		Peers:   peers,
		Usage:   &fingerprint.PeerUsageCache{Peers: peers, Client: &http.Client{Timeout: 8 * time.Second}},
	}
	gate.RefreshView()

	old := fingerprintPeerGate
	fingerprintPeerGate = gate
	t.Cleanup(func() { fingerprintPeerGate = old })
	return hits
}

// newGuardLedger builds a ledger with every production callback installed.
func newGuardLedger(t *testing.T) *capacity.Ledger {
	t.Helper()
	bo := autonomous.NewBackoff()
	bl := autonomous.NewBudgetLedger(filepath.Join(t.TempDir(), "budget.json"))
	bl.Preload()
	var gpuMu sync.Mutex
	return capacity.New(capacity.Options{
		External:     hostExternalCount(guardLister{}),
		Backpressure: gpuNodeBackpressure(func() int { return 90 }, &gpuMu, map[string]float64{}),
		AutonomousBackpressure: composeBackpressure(
			backoffBackpressure(bo),
			budgetBackpressure(bl),
		),
		HeldBackpressure: sharedNodeBackpressure(),
		PollInterval:     50 * time.Millisecond,
	})
}

var guardPools = []string{"host", "llm:guard", "node:gpu-0"}

// guardWaiters registers one waiter per pool kind behind a holder that owns
// every pool, and returns the channels that report each Acquire's result.
func guardWaiters(t *testing.T, ctx context.Context, led *capacity.Ledger) map[string]chan error {
	t.Helper()
	for _, p := range guardPools {
		led.SetLimit(p, 1)
	}
	if err := led.Acquire(ctx, capacity.Request{Holder: "h0", PRDID: "p0", Pools: guardPools}, time.Second, nil, nil); err != nil {
		t.Fatalf("first holder: %v", err)
	}
	results := map[string]chan error{}
	for i, p := range guardPools {
		res := make(chan error, 1)
		results[p] = res
		waiting := make(chan struct{})
		holder := "w" + string(rune('0'+i))
		go func(p string) {
			res <- led.Acquire(ctx, capacity.Request{Holder: holder, PRDID: "p" + holder, Pools: []string{p}}, 4*time.Second, nil, func(string) {
				select {
				case <-waiting:
				default:
					close(waiting)
				}
			})
		}(p)
		select {
		case <-waiting:
		case <-time.After(guardDeadline):
			t.Fatalf("waiter on %s never started waiting", p)
		}
	}
	return results
}

func TestCapacityCallbackGuardLedgerStaysResponsiveWithSlowPeer(t *testing.T) {
	hits := slowPeerGate(t)
	led := newGuardLedger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := guardWaiters(t, ctx, led)

	for i := 0; i < 20; i++ {
		mustComplete(t, "Snapshot", func() { _ = led.Snapshot() })
		mustComplete(t, "SetLimit", func() { led.SetLimit("host", 1) })
		mustComplete(t, "Release of unknown holder", func() { led.Release("nobody") })
		mustComplete(t, "Acquire", func() {
			err := led.Acquire(ctx, capacity.Request{Holder: "other", PRDID: "po", Pools: guardPools}, 20*time.Millisecond, nil, nil)
			if !errors.Is(err, capacity.ErrWaitTimeout) {
				t.Errorf("Acquire for a full pool: err = %v, want wait timeout", err)
			}
		})
	}

	// Releasing the holder dispatches the waiters. The node waiter's gate
	// consults the slow peer's cache; it must be granted without waiting.
	start := time.Now()
	mustComplete(t, "Release", func() { led.Release("h0") })
	for pool, res := range results {
		select {
		case err := <-res:
			if err != nil {
				t.Errorf("waiter on %s: %v, want granted", pool, err)
			}
		case <-time.After(guardDeadline):
			t.Fatalf("waiter on %s was not granted within %s of the release", pool, guardDeadline)
		}
	}
	if d := time.Since(start); d >= 3*time.Second {
		t.Fatalf("dispatch took %s: it waited for the slow peer", d)
	}
	deadline := time.Now().Add(guardDeadline)
	for atomic.LoadInt32(hits) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(hits) == 0 {
		t.Fatal("the slow peer was never asked: the test does not exercise the gate")
	}
	mustComplete(t, "Snapshot after grants", func() { _ = led.Snapshot() })
}

func TestCapacityCallbackGuardDispatchDoesNotWaitForPeer(t *testing.T) {
	slowPeerGate(t)
	led := newGuardLedger(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	led.SetLimit("node:gpu-0", 1)
	if err := led.Acquire(ctx, capacity.Request{Holder: "a", PRDID: "pa", Pools: []string{"node:gpu-0"}}, time.Second, nil, nil); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	reasons := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- led.Acquire(ctx, capacity.Request{Holder: "b", PRDID: "pb", Pools: []string{"node:gpu-0"}}, 4*time.Second, nil, func(r string) { reasons <- r })
	}()
	select {
	case <-reasons:
	case <-time.After(guardDeadline):
		t.Fatal("second request never started waiting")
	}
	start := time.Now()
	mustComplete(t, "Release", func() { led.Release("a") })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("node waiter: %v, want granted", err)
		}
	case <-time.After(guardDeadline):
		t.Fatalf("node waiter not granted within %s of the release", guardDeadline)
	}
	if d := time.Since(start); d >= 2*time.Second {
		t.Fatalf("grant took %s: dispatch waited for the peer", d)
	}
}

// TestCapacityCallbackGuardHarnessCatchesReentrantCallback installs, for each
// callback in turn, a version that calls Snapshot from another goroutine and
// waits for it. That deadlocks while the ledger lock is held, and the guard
// harness must report the blocked ledger.
func TestCapacityCallbackGuardHarnessCatchesReentrantCallback(t *testing.T) {
	for _, which := range []string{"External", "Backpressure", "AutonomousBackpressure", "HeldBackpressure"} {
		t.Run(which, func(t *testing.T) {
			var led *capacity.Ledger
			var armed atomic.Bool
			entered := make(chan struct{})
			reenter := func() {
				if !armed.CompareAndSwap(false, true) {
					return
				}
				close(entered)
				snap := make(chan struct{})
				go func() { _ = led.Snapshot(); close(snap) }()
				select {
				case <-snap:
				case <-time.After(1500 * time.Millisecond): // bound the deadlock so the test cleans up
				}
			}
			var opt capacity.Options
			switch which {
			case "External":
				opt.External = func(string) int { reenter(); return 0 }
			case "Backpressure":
				opt.Backpressure = func(string) (bool, string) { reenter(); return false, "" }
			case "AutonomousBackpressure":
				opt.AutonomousBackpressure = func(string) (bool, string) { reenter(); return false, "" }
			case "HeldBackpressure":
				opt.HeldBackpressure = func(string, int, int) (bool, string) { reenter(); return false, "" }
			}
			led = capacity.New(opt)
			led.SetLimit("node:x", 5)
			acquired := make(chan struct{})
			go func() {
				_ = led.Acquire(context.Background(), capacity.Request{Holder: "h", PRDID: "p", Pools: []string{"node:x"}}, time.Second, nil, nil)
				close(acquired)
			}()
			select {
			case <-entered:
			case <-time.After(guardDeadline):
				t.Fatalf("%s callback was never invoked", which)
			}
			if completesWithin(300*time.Millisecond, func() { _ = led.Snapshot() }) {
				t.Fatalf("harness did not detect a %s callback that re-enters the ledger", which)
			}
			<-acquired // the bounded deadlock ends and the ledger recovers
		})
	}
}
