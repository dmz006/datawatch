package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

var drainPoolClock = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// drainedPoolStatus is a node pool the daemon reports as drained.
func drainedPoolStatus(until time.Time, held int) capacity.PoolStatus {
	return capacity.PoolStatus{
		Name:       "node:gpu-a",
		Limit:      2,
		Held:       held,
		Drained:    true,
		DrainUntil: until,
	}
}

func TestFormatPoolsDrainedNodePoolShowsDrain(t *testing.T) {
	until := drainPoolClock.Add(time.Hour)
	got := capacity.FormatPools([]capacity.PoolStatus{
		namePool("host", 2, 1),
		drainedPoolStatus(until, 1),
	})
	nodeLine := line(t, got, "node:gpu-a")
	if !strings.Contains(nodeLine, "drained until "+until.UTC().Format(time.RFC3339)) {
		t.Fatalf("drained pool line %q lacks the drain time", nodeLine)
	}
	// Non-drained pools must not pick up drain wording.
	hostLine := line(t, got, "host")
	if strings.Contains(hostLine, "drained") {
		t.Fatalf("undrained host pool shows drain wording: %q", hostLine)
	}
	// A drained pool still shows how much is holding it.
	if !strings.Contains(nodeLine, "1/2") {
		t.Fatalf("drained pool line lost its used/limit figures: %q", nodeLine)
	}
}

func TestFormatPoolsDrainedWithTasksStillFinishing(t *testing.T) {
	got := capacity.FormatPools([]capacity.PoolStatus{drainedPoolStatus(drainPoolClock.Add(time.Hour), 1)})
	nodeLine := line(t, got, "node:gpu-a")
	if !strings.Contains(nodeLine, "1 task still finishing") {
		t.Fatalf("singular finishing count missing: %q", nodeLine)
	}

	got = capacity.FormatPools([]capacity.PoolStatus{drainedPoolStatus(time.Time{}, 2)})
	nodeLine = line(t, got, "node:gpu-a")
	// Open-ended drain: "drained" alone, no time.
	if !strings.Contains(nodeLine, " drained") || strings.Contains(nodeLine, "drained until") {
		t.Fatalf("open-ended drain wording wrong: %q", nodeLine)
	}
	if !strings.Contains(nodeLine, "2 tasks still finishing") {
		t.Fatalf("plural finishing count missing: %q", nodeLine)
	}
}

func TestFormatPoolsDrainedEmptyPool(t *testing.T) {
	// A drained pool with nothing holding shows the drain but no finishing note.
	got := capacity.FormatPools([]capacity.PoolStatus{drainedPoolStatus(drainPoolClock.Add(time.Hour), 0)})
	nodeLine := line(t, got, "node:gpu-a")
	if !strings.Contains(nodeLine, "drained until ") {
		t.Fatalf("drain wording missing: %q", nodeLine)
	}
	if strings.Contains(nodeLine, "still finishing") {
		t.Fatalf("empty drained pool must not show finishing tasks: %q", nodeLine)
	}
}

func TestFormatPoolsUndrainedPoolUnchanged(t *testing.T) {
	got := capacity.FormatPools([]capacity.PoolStatus{{Name: "node:gpu-a", Limit: 2, Held: 1}})
	nodeLine := line(t, got, "node:gpu-a")
	if strings.Contains(nodeLine, "drained") {
		t.Fatalf("undrained pool must not show drain wording: %q", nodeLine)
	}
	if !strings.Contains(nodeLine, "1/2") {
		t.Fatalf("used/limit figures missing: %q", nodeLine)
	}
}

// namePool is a non-node pool for the mixed-pool cases.
func namePool(name string, limit, held int) capacity.PoolStatus {
	return capacity.PoolStatus{Name: name, Limit: limit, Held: held}
}

// line returns the rendered line for the named pool (fatal when absent).
func line(t *testing.T, got, name string) string {
	t.Helper()
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, name+": ") {
			return l
		}
	}
	t.Fatalf("no line for pool %s in:\n%s", name, got)
	return ""
}
