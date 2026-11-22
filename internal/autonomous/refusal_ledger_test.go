package autonomous

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

type refusalClock struct{ t time.Time }

func (c *refusalClock) now() time.Time { c.t = c.t.Add(time.Second); return c.t }

func newRefusalClock() *refusalClock { return &refusalClock{t: time.Now()} }

func TestRefusalLedgerNewAndRepeat(t *testing.T) {
	c := newRefusalClock()
	l := NewRefusalLedger("")
	if !l.Record("a1", RefusalStepArchive, "landing pending", "fp1", c.now()) {
		t.Fatal("first record must be new")
	}
	if l.Record("a1", RefusalStepArchive, "landing pending", "fp1", c.now()) {
		t.Fatal("identical repeat must not be new")
	}
	if !l.Suppressed("a1", RefusalStepArchive, "landing pending", "fp1") {
		t.Fatal("expected suppressed")
	}
	got := l.List()
	if len(got) != 1 || got[0].Repeats != 1 || !got[0].LastSeen.After(got[0].FirstSeen) {
		t.Fatalf("unexpected entry %+v", got)
	}
}

func TestRefusalLedgerChangeIsNew(t *testing.T) {
	c := newRefusalClock()
	l := NewRefusalLedger("")
	l.Record("a1", RefusalStepRepair, "r", "fp", c.now())
	if !l.Record("a1", RefusalStepRepair, "r2", "fp", c.now()) {
		t.Fatal("changed reason is new")
	}
	if !l.Record("a1", RefusalStepRepair, "r2", "fp2", c.now()) {
		t.Fatal("changed fingerprint is new")
	}
	if l.Suppressed("a1", RefusalStepRepair, "r2", "fp") {
		t.Fatal("stale fingerprint must not suppress")
	}
	if l.List()[0].Repeats != 0 {
		t.Fatal("replaced entry restarts the counter")
	}
}

func TestRefusalLedgerClearPrune(t *testing.T) {
	c := newRefusalClock()
	l := NewRefusalLedger("")
	l.Record("a1", RefusalStepArchive, "r", "f", c.now())
	l.Record("a1", RefusalStepMerge, "r", "f", c.now())
	l.Record("a2", RefusalStepArchive, "r", "f", c.now())
	l.Clear("a1", RefusalStepArchive)
	if l.Suppressed("a1", RefusalStepArchive, "r", "f") || len(l.List()) != 2 {
		t.Fatal("clear failed")
	}
	l.Prune([]string{"a2"})
	got := l.List()
	if len(got) != 1 || got[0].AutomatonID != "a2" {
		t.Fatalf("prune failed: %+v", got)
	}
}

func TestRefusalLedgerPersistence(t *testing.T) {
	c := newRefusalClock()
	p := filepath.Join(t.TempDir(), "sub", "refusals.json")
	l := NewRefusalLedger(p)
	l.Record("a1", RefusalStepArchive, "r", "f", c.now())
	l.Record("a1", RefusalStepArchive, "r", "f", c.now())
	l2 := NewRefusalLedger(p)
	got := l2.List()
	if len(got) != 1 || got[0].Repeats != 1 || !l2.Suppressed("a1", RefusalStepArchive, "r", "f") {
		t.Fatalf("round trip failed: %+v", got)
	}
}

func TestRefusalLedgerBoundedAndMapLookup(t *testing.T) {
	c := newRefusalClock()
	l := NewRefusalLedger("")
	for i := 0; i < 5000; i++ {
		l.Record(fmt.Sprintf("auto-%d", i), RefusalStepArchive, "r", "f", c.now())
	}
	if n := len(l.List()); n != refusalLedgerCap {
		t.Fatalf("List length %d, want cap %d", n, refusalLedgerCap)
	}
	// Lookup is a single map hit keyed by (Automaton, step); the map never exceeds the cap.
	if len(l.entries) != refusalLedgerCap {
		t.Fatalf("map size %d", len(l.entries))
	}
	if !l.Suppressed("auto-4999", RefusalStepArchive, "r", "f") {
		t.Fatal("newest Automaton entry must be present")
	}
	if l.Suppressed("auto-0", RefusalStepArchive, "r", "f") {
		t.Fatal("oldest entry must have been dropped")
	}
}
