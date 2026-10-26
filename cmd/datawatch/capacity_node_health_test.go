package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

func TestNodeHealthViewHealthyBeforeFirstRefresh(t *testing.T) {
	v := newNodeHealthView()
	if !v.healthy("anything") {
		t.Fatal("a node must count as healthy until the registry has been read")
	}
}

func TestNodeHealthViewRegistryAndMaintenance(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	v := newNodeHealthView()
	v.now = func() time.Time { return now }
	v.refresh([]*compute.Node{
		{Name: "up"},
		{Name: "down", MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(-time.Hour), To: now.Add(time.Hour)}}},
		{Name: "open", MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(-time.Hour)}}},
		{Name: "later", MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(time.Hour), To: now.Add(2 * time.Hour)}}},
		{Name: "past", MaintenanceWindows: []compute.MaintenanceWindow{{From: now.Add(-2 * time.Hour), To: now.Add(-time.Hour)}}},
	})
	for name, want := range map[string]bool{
		"up": true, "down": false, "open": false, "later": true, "past": true, "gone": false,
	} {
		if got := v.healthy(name); got != want {
			t.Errorf("healthy(%q) = %v, want %v", name, got, want)
		}
	}

	// A window that opens between refreshes counts at once.
	now = now.Add(90 * time.Minute)
	if v.healthy("later") {
		t.Error("healthy(later) = true inside its maintenance window")
	}
}

// The view feeds a real ledger: a pooled request skips the node in
// maintenance and names it in the wait reason.
func TestNodeHealthViewDrivesLedgerPlacement(t *testing.T) {
	v := newNodeHealthView()
	v.refresh([]*compute.Node{
		{Name: "a"},
		{Name: "b", MaintenanceWindows: []compute.MaintenanceWindow{{From: time.Now().Add(-time.Hour)}}},
	})
	led := capacity.New(capacity.Options{NodeHealthy: v.healthy})
	led.SetLimit("node:a", 1)
	led.SetLimit("node:b", 1)
	req := func(holder string) capacity.Request {
		return capacity.Request{
			Holder: holder, PRDID: "p", Pools: []string{"host"},
			NodeChoices: []capacity.NodeChoice{{Name: "a", Priority: 1}, {Name: "b", Priority: 9}},
		}
	}
	if err := led.Acquire(context.Background(), req("t1"), 0, nil, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if ls := led.LeaseOf("t1"); ls == nil || ls.Node != "a" {
		t.Fatalf("lease = %+v, want node a because b is in maintenance", ls)
	}
	d := led.Check(req("t2"))
	if d.Kind == capacity.DecisionAdmit || !strings.Contains(d.Reason, "b: unhealthy") {
		t.Fatalf("Check = %+v, want a queued decision naming b as unhealthy", d)
	}
}
