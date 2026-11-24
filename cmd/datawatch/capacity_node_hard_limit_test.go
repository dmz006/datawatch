package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

func TestComputeNodeFromConfigCarriesHardLimit(t *testing.T) {
	if computeNodeFromConfig(config.ComputeNodeYAML{Name: "a"}).HardLimit {
		t.Fatal("hard_limit must default to off")
	}
	if !computeNodeFromConfig(config.ComputeNodeYAML{Name: "a", HardLimit: true}).HardLimit {
		t.Fatal("hard_limit from config.yaml was dropped")
	}
}

// Each node's own setting reaches the ledger: the hard node makes an operator
// start wait, the soft node (default) admits it over the pool and records the
// overage. A node switched back to soft is soft again on the next sync.
func TestSyncNodeHardLimitsAppliesPerNode(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: 5 * time.Millisecond})
	led.SetLimit("node:hard", 1)
	led.SetLimit("node:soft", 1)
	nodes := []*compute.Node{{Name: "hard", HardLimit: true}, {Name: "soft"}}
	syncNodeHardLimits(led, nodes)

	take := func(holder, node string, class capacity.Class) error {
		return led.Acquire(context.Background(), capacity.Request{Holder: holder, Class: class, Pools: []string{"node:" + node}, Node: node}, 50*time.Millisecond, nil, nil)
	}
	for _, n := range []string{"hard", "soft"} {
		if err := take("w-"+n, n, capacity.ClassWorker); err != nil {
			t.Fatal(err)
		}
	}
	if err := take("op-soft", "soft", capacity.ClassOperator); err != nil {
		t.Fatalf("soft node: operator start refused: %v", err)
	}
	if ls := led.LeaseOf("op-soft"); ls == nil || len(ls.Overage) != 1 || ls.Overage[0].Pool != "node:soft" {
		t.Fatalf("soft node lease = %+v, want one overage on node:soft", ls)
	}
	if err := take("op-hard", "hard", capacity.ClassOperator); !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("hard node: err = %v, want wait timeout", err)
	}
	nodes[0].HardLimit = false
	syncNodeHardLimits(led, nodes)
	if err := take("op-hard", "hard", capacity.ClassOperator); err != nil {
		t.Fatalf("hard node after switching it off: %v", err)
	}
}
