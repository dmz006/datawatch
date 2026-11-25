package orchestrator

import (
	"testing"
	"time"
)

// A graph or node id the store already holds is never issued again, even when
// the clock reads the same value.
func TestIDReuse_GraphAndNodeIDs(t *testing.T) {
	old := idClock
	at := time.Date(2026, 11, 20, 8, 26, 15, 0, time.UTC)
	idClock = func() time.Time { return at }
	t.Cleanup(func() { idClock = old })

	r := newTest(t, nil, nil)
	g1, err := r.Store().CreateGraph("one", "", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	g2, err := r.Store().CreateGraph("two", "", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID == g2.ID {
		t.Fatalf("second graph was given the first one's id %s", g1.ID)
	}
	if got, ok := r.Store().GetGraph(g1.ID); !ok || got.Title != "one" {
		t.Fatalf("graph %s now holds %+v", g1.ID, got)
	}
	seen := map[string]bool{g1.ID: true, g2.ID: true}
	for _, id := range []string{g1.ID, g2.ID} {
		planned, err := r.Plan(id, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range planned.Nodes {
			if seen[n.ID] {
				t.Fatalf("node id %s issued twice", n.ID)
			}
			seen[n.ID] = true
		}
	}
}
