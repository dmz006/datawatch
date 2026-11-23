package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
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

func TestRefusalLedgerNilReceiver(t *testing.T) {
	var l *RefusalLedger
	if l.Record("a1", RefusalStepArchive, "r", "f", time.Now()) {
		t.Fatal("nil ledger must not record")
	}
	if l.Suppressed("a1", RefusalStepArchive, "r", "f") {
		t.Fatal("nil ledger must not suppress")
	}
	if got := l.List(); got != nil {
		t.Fatalf("nil ledger List must be nil, got %v", got)
	}
	l.Clear("a1", RefusalStepArchive) // must not panic
	l.Prune([]string{"a1"})           // must not panic
}

func TestRefusalLedgerListNewestFirst(t *testing.T) {
	c := newRefusalClock()
	l := NewRefusalLedger("")
	l.Record("a1", RefusalStepArchive, "r", "f", c.now())
	l.Record("a2", RefusalStepArchive, "r", "f", c.now())
	l.Record("a3", RefusalStepArchive, "r", "f", c.now())
	got := l.List()
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].AutomatonID != "a3" || got[1].AutomatonID != "a2" || got[2].AutomatonID != "a1" {
		t.Fatalf("entries must be sorted newest first: %+v", got)
	}
	// A repeat moves the Automaton to the front of the list.
	l.Record("a1", RefusalStepArchive, "r", "f", c.now())
	if got := l.List(); got[0].AutomatonID != "a1" {
		t.Fatalf("repeated entry must come first: %+v", got)
	}
	// Equal times fall back to the Automaton id, then the step, so the
	// order stays deterministic across Automata and steps.
	at := c.now()
	l.Record("a2", RefusalStepArchive, "r", "f", at)
	l.Record("a1", RefusalStepMerge, "r", "f", at)
	l.Record("a1", RefusalStepArchive, "r", "f", at)
	got = l.List()
	want := []struct{ id, step string }{{"a1", RefusalStepArchive}, {"a1", RefusalStepMerge}, {"a2", RefusalStepArchive}, {"a3", RefusalStepArchive}}
	if len(got) != len(want) {
		t.Fatalf("want %d entries, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].AutomatonID != w.id || got[i].Step != w.step {
			t.Fatalf("tie-break order at %d: got %s/%s, want %s/%s", i, got[i].AutomatonID, got[i].Step, w.id, w.step)
		}
	}
}

func TestRefusalLedgerLoadTruncatesOverCap(t *testing.T) {
	c := newRefusalClock()
	p := filepath.Join(t.TempDir(), "refusals.json")
	const n = refusalLedgerCap + 100 // a persisted ledger larger than the cap
	list := make([]Refusal, 0, n)
	for i := 0; i < n; i++ {
		at := c.now()
		list = append(list, Refusal{AutomatonID: fmt.Sprintf("auto-%d", i), Step: RefusalStepMerge,
			Reason: "r", Fingerprint: "f", FirstSeen: at, LastSeen: at})
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewRefusalLedger(p)
	if got := len(l.List()); got != refusalLedgerCap {
		t.Fatalf("loaded ledger must be capped at %d, got %d", refusalLedgerCap, got)
	}
	// The oldest (least recently seen) Automata are the ones dropped.
	if !l.Suppressed(fmt.Sprintf("auto-%d", n-1), RefusalStepMerge, "r", "f") {
		t.Fatal("newest Automaton entry must survive the load")
	}
	if l.Suppressed("auto-0", RefusalStepMerge, "r", "f") {
		t.Fatal("oldest Automaton entry must have been dropped on the load")
	}
}
