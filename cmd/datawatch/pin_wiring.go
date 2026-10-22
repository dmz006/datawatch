package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pinkeeper"
)

// startPinKeepers starts one pin keeper for each compute node that has a
// node_pinned_model. Each keeper checks its node every pin_check_seconds
// (0 turns them all off). The node is busy while the admission ledger holds
// a lease on it. It returns a function that stops every keeper started.
func startPinKeepers(ctx context.Context, cfg *config.Config, reg *compute.Registry, store *alerts.Store, led *capacity.Ledger) func() {
	if reg == nil || cfg.PinCheckSeconds <= 0 {
		return func() {}
	}
	interval := time.Duration(cfg.PinCheckSeconds) * time.Second
	busy := pinkeeper.LedgerBusy(led)
	logf := func(format string, args ...any) {
		fmt.Printf("[pin] "+format+"\n", args...)
	}
	var keepers []*pinkeeper.PinKeeper
	for _, n := range reg.List() {
		if n.NodePinnedModel == "" {
			continue
		}
		if n.Address == "" {
			logf("node %s: pinned to %s but has no address; not pinning", n.Name, n.NodePinnedModel)
			continue
		}
		k := pinkeeper.New(pinkeeper.Options{
			Node:     n.Name,
			Address:  n.Address,
			Model:    n.NodePinnedModel,
			Interval: interval,
			Busy:     busy,
			Alerts:   store,
			Logf:     logf,
		})
		k.Start(ctx)
		keepers = append(keepers, k)
	}
	return func() {
		for _, k := range keepers {
			k.Stop()
		}
	}
}
