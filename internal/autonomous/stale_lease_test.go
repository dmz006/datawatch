package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

// A lease granted on a node that has left the candidate list is stale; one on
// a listed node, or on the single current node, is not.
func TestLeaseNodeStaleFollowsCurrentList(t *testing.T) {
	led := capacity.New(capacity.Options{})
	m := &Manager{}
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"host"}, "" })
	if err := led.Acquire(t.Context(), capacity.Request{Holder: "t1", Pools: []string{"host", "node:a"}, Node: "a"}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	list := []capacity.NodeChoice{{Name: "a"}, {Name: "b"}}
	m.SetCapacityChoices(func(string, string) []capacity.NodeChoice { return list })
	if m.leaseNodeStale("t1", "pi", "m") {
		t.Fatal("lease on a listed node reported stale")
	}
	list = []capacity.NodeChoice{{Name: "b"}, {Name: "c"}}
	if !m.leaseNodeStale("t1", "pi", "m") {
		t.Fatal("lease on a removed node not reported stale")
	}
	list = nil
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"host"}, "b" })
	if !m.leaseNodeStale("t1", "pi", "m") {
		t.Fatal("lease differing from the single current node not reported stale")
	}
}
