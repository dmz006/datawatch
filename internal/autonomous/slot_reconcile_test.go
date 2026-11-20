package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// orphanFakeLedger is an in-test ledger. It also records session-ending calls:
// the reconcile has no way to reach one, so endCalls must stay zero.
type orphanFakeLedger struct {
	leases   map[string]capacity.Lease
	released []string
	endCalls int
}

func (f *orphanFakeLedger) Leases() []capacity.Lease {
	out := make([]capacity.Lease, 0, len(f.leases))
	for _, l := range f.leases {
		out = append(out, l)
	}
	return out
}

func (f *orphanFakeLedger) ReleaseIf(h string, a time.Time) bool {
	l, ok := f.leases[h]
	if !ok || !l.Acquired.Equal(a) {
		return false
	}
	delete(f.leases, h)
	f.released = append(f.released, h)
	return true
}

// EndSession exists only so the fake can record an unexpected call.
func (f *orphanFakeLedger) EndSession(string) { f.endCalls++ }

type orphanFakeChecks struct{ live, node, done map[string]bool }

func (c orphanFakeChecks) SessionLive(id string) bool         { return c.live[id] }
func (c orphanFakeChecks) HasNodeLease(l capacity.Lease) bool { return c.node[l.Holder] }
func (c orphanFakeChecks) AutomatonFinished(u string) bool    { return c.done[u] }

func orphanLease(holder, session string) capacity.Lease {
	return capacity.Lease{
		Holder:    holder,
		Pools:     []string{PiPromptPool},
		Acquired:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		SessionID: session,
	}
}

func orphanLedger(ls ...capacity.Lease) *orphanFakeLedger {
	f := &orphanFakeLedger{leases: map[string]capacity.Lease{}}
	for _, l := range ls {
		f.leases[l.Holder] = l
	}
	return f
}

func TestSlotReconcileFreesJudgeAfterMerge(t *testing.T) {
	led := orphanLedger(orphanLease("judge:u1", "s1"))
	chk := orphanFakeChecks{live: map[string]bool{}, node: map[string]bool{"judge:u1": true}, done: map[string]bool{"u1": true}}
	got := ReconcileOrphanSlots(led, chk, chk, chk)
	if len(got) != 1 {
		t.Fatalf("freed %d, want 1: %+v", len(got), got)
	}
	want := FreedSlot{Holder: "judge:u1", Pool: PiPromptPool, Reason: SlotReasonAutomatonFinished}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
	if len(led.leases) != 0 {
		t.Fatalf("lease still held: %+v", led.leases)
	}
}

func TestSlotReconcileFreesVerifierWithNoLease(t *testing.T) {
	led := orphanLedger(orphanLease("verify:u2", "s2"))
	chk := orphanFakeChecks{live: map[string]bool{}, node: map[string]bool{}, done: map[string]bool{}}
	got := ReconcileOrphanSlots(led, chk, chk, chk)
	if len(got) != 1 {
		t.Fatalf("freed %d, want 1: %+v", len(got), got)
	}
	want := FreedSlot{Holder: "verify:u2", Pool: PiPromptPool, Reason: SlotReasonNoSessionNoLease}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
	if len(led.leases) != 0 {
		t.Fatalf("lease still held")
	}
}

func TestSlotReconcileLiveSessionNeverFreed(t *testing.T) {
	cases := []struct {
		name        string
		lease, done bool
	}{
		{"lease present", true, false},
		{"lease absent", false, false},
		{"automaton finished", true, true},
		{"finished and no lease", false, true},
	}
	for _, c := range cases {
		for _, holder := range []string{"judge:u3", "verify:u3"} {
			t.Run(c.name+"/"+holder, func(t *testing.T) {
				led := orphanLedger(orphanLease(holder, "live"))
				chk := orphanFakeChecks{
					live: map[string]bool{"live": true},
					node: map[string]bool{holder: c.lease},
					done: map[string]bool{"u3": c.done},
				}
				got := ReconcileOrphanSlots(led, chk, chk, chk)
				if len(got) != 0 || len(led.released) != 0 {
					t.Fatalf("released a live holder: %+v %v", got, led.released)
				}
				if _, ok := led.leases[holder]; !ok {
					t.Fatalf("lease for %s gone", holder)
				}
			})
		}
	}
}

func TestSlotReconcileTwiceFreesOnce(t *testing.T) {
	led := orphanLedger(orphanLease("judge:u4", "s4"), orphanLease("verify:u5", "s5"))
	chk := orphanFakeChecks{live: map[string]bool{}, node: map[string]bool{}, done: map[string]bool{"u4": true}}
	first := ReconcileOrphanSlots(led, chk, chk, chk)
	second := ReconcileOrphanSlots(led, chk, chk, chk)
	if len(first) != 2 {
		t.Fatalf("first pass freed %d, want 2", len(first))
	}
	if len(second) != 0 {
		t.Fatalf("second pass freed %+v, want none", second)
	}
	if len(led.released) != 2 {
		t.Fatalf("ledger released %v, want 2 releases total", led.released)
	}
}

func TestOrphanSlotReconcileNeverEndsSession(t *testing.T) {
	led := orphanLedger(
		orphanLease("judge:a", "sa"),
		orphanLease("verify:b", "sb"),
		orphanLease("judge:c", "live"),
	)
	chk := orphanFakeChecks{live: map[string]bool{"live": true}, node: map[string]bool{}, done: map[string]bool{"a": true}}
	ReconcileOrphanSlots(led, chk, chk, chk)
	ReconcileOrphanSlots(led, chk, chk, chk)
	if led.endCalls != 0 {
		t.Fatalf("session-ending call made %d times", led.endCalls)
	}
}

func TestOrphanSlotReconcileSkipsReplacedLease(t *testing.T) {
	led := orphanLedger(orphanLease("verify:u6", "s6"))
	chk := orphanFakeChecks{live: map[string]bool{}, node: map[string]bool{}, done: map[string]bool{}}
	// The lease list is a copy; replace the lease before ReleaseIf runs.
	stale := led.Leases()[0]
	repl := stale
	repl.Acquired = stale.Acquired.Add(time.Minute)
	led.leases[stale.Holder] = repl
	if led.ReleaseIf(stale.Holder, stale.Acquired) {
		t.Fatal("released a replaced lease")
	}
	if got := ReconcileOrphanSlots(led, chk, chk, chk); len(got) != 1 {
		t.Fatalf("current lease should free once, got %+v", got)
	}
}
