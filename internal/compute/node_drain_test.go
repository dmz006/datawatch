package compute

import (
	"encoding/json"
	"testing"
	"time"
)

// TestNodeDrainActiveFixedTimes verifies DrainActive with fixed times:
// active before until, not active at and after until, and an open-ended
// drain that stays active indefinitely.
func TestNodeDrainActiveFixedTimes(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	until := now.Add(time.Hour)

	n := &Node{Drained: true, DrainUntil: until, DrainReason: "maintenance"}

	if !n.DrainActive(now.Add(-time.Minute)) {
		t.Fatal("drain should be active before until")
	}
	if !n.DrainActive(now) {
		t.Fatal("drain should be active at now (before until)")
	}
	if n.DrainActive(until) {
		t.Fatal("drain should not be active at until")
	}
	if n.DrainActive(until.Add(time.Minute)) {
		t.Fatal("drain should not be active after until")
	}
	// Not drained = never active.
	plain := &Node{}
	if plain.DrainActive(now) {
		t.Fatal("undrained node must not report drain active")
	}
	// Drained but not yet due? Drain is a state, not a future schedule —
	// Drained+until in the future is active now; the "not started" case
	// simply is !Drained.
}

// TestNodeDrainOpenEnded verifies an open-ended drain (zero DrainUntil)
// stays active across time.
func TestNodeDrainOpenEnded(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	n := &Node{Drained: true, DrainReason: "hardware swap"}
	if !n.DrainActive(now) {
		t.Fatal("open-ended drain should be active")
	}
	if !n.DrainActive(now.Add(30 * 24 * time.Hour)) {
		t.Fatal("open-ended drain should stay active indefinitely")
	}
}

// TestNodeDrainJSONRoundTrip marshals a drained node to JSON, unmarshals
// it back, and checks all three drain fields survive. Also checks
// omitempty: a plain node serializes without drain keys.
func TestNodeDrainJSONRoundTrip(t *testing.T) {
	until := time.Date(2026, 12, 1, 9, 30, 0, 0, time.UTC)
	in := &Node{
		Name:        "n1",
		Drained:     true,
		DrainUntil:  until,
		DrainReason: "gpu memory leak",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Node
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Drained {
		t.Fatal("drained lost in round trip")
	}
	if !out.DrainUntil.Equal(until) {
		t.Fatalf("drain_until lost in round trip: %v", out.DrainUntil)
	}
	if out.DrainReason != "gpu memory leak" {
		t.Fatalf("drain_reason lost in round trip: %q", out.DrainReason)
	}
	// omitempty: a plain node serializes without drained/drain_reason
	// keys. (drain_until is a time.Time struct, so Go always serializes
	// it even when zero — the zero value is the documented "no end time".)
	plain, _ := json.Marshal(&Node{Name: "p", Kind: KindOllama})
	var m map[string]interface{}
	_ = json.Unmarshal(plain, &m)
	for _, k := range []string{"drained", "drain_reason"} {
		if _, ok := m[k]; ok {
			t.Fatalf("plain node should omit %q (omitempty)", k)
		}
	}
}
