package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

type fakeSlots struct{ leases map[string]capacity.Lease }

func (f *fakeSlots) Leases() []capacity.Lease {
	var out []capacity.Lease
	for _, l := range f.leases {
		out = append(out, l)
	}
	return out
}
func (f *fakeSlots) ReleaseIf(h string, a time.Time) bool {
	l, ok := f.leases[h]
	if !ok || !l.Acquired.Equal(a) {
		return false
	}
	delete(f.leases, h)
	return true
}

type fakeChecks struct{ live, node, done map[string]bool }

func (c fakeChecks) SessionLive(id string) bool         { return c.live[id] }
func (c fakeChecks) HasNodeLease(l capacity.Lease) bool { return c.node[l.Holder] }
func (c fakeChecks) AutomatonFinished(unit string) bool { return c.done[unit] }

func TestReconcileOrphanSlots(t *testing.T) {
	now := time.Now()
	mk := func(h, sid string) capacity.Lease {
		return capacity.Lease{Holder: h, Pools: []string{PiPromptPool}, SessionID: sid, Acquired: now}
	}
	f := &fakeSlots{leases: map[string]capacity.Lease{
		"verify:a": mk("verify:a", ""),  // orphan
		"judge:b":  mk("judge:b", "s1"), // live session, no node lease: kept
		"verify:c": mk("verify:c", ""),  // finished
		"judge:d":  mk("judge:d", ""),   // has node lease: kept
		"worker":   mk("worker", ""),    // not judge/verifier: kept
	}}
	ch := fakeChecks{live: map[string]bool{"s1": true}, node: map[string]bool{"judge:d": true}, done: map[string]bool{"c": true}}
	got := ReconcileOrphanSlots(f, ch, ch, ch)
	if len(got) != 2 || got[0].Holder != "verify:a" || got[0].Reason != SlotReasonNoSessionNoLease ||
		got[1].Holder != "verify:c" || got[1].Reason != SlotReasonAutomatonFinished {
		t.Fatalf("unexpected: %+v", got)
	}
	if again := ReconcileOrphanSlots(f, ch, ch, ch); len(again) != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
	if _, ok := f.leases["judge:b"]; !ok {
		t.Fatal("live session freed")
	}
}
