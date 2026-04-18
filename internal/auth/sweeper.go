// F10 sprint 5 (S5.5) — periodic orphan-token sweeper.
//
// agents.Manager.Terminate already revokes the worker's token on the
// happy path (S5.3). The sweeper is the safety net for everything
// else: parent crashes mid-revoke, worker disappears without a
// Terminate call, tokens whose ExpiresAt has passed.
//
// Run from a single goroutine at daemon startup with a 5-min cadence
// (default). The activeIDsFn closure lets callers plug in
// agents.Manager.ActiveIDs without an import edge.

package auth

import (
	"context"
	"fmt"
	"io"
	"time"
)

// SweeperConfig captures the knobs for RunSweeper. Fields are
// optional; sensible defaults apply when zero.
type SweeperConfig struct {
	Broker        *TokenBroker
	ActiveIDsFn   func() []string // returns IDs of currently-alive workers
	Interval      time.Duration   // default 5 min
	OnSweepError  func(error)     // optional error sink (test hooks)
	OnSweepResult func(swept int) // optional result sink (test hooks)
}

// RunSweeper blocks until ctx is cancelled, calling
// Broker.SweepOrphans(ActiveIDsFn()) every Interval. Returns when
// ctx is done. Safe to call from a goroutine; not safe to call
// twice concurrently against the same broker (would race the
// store mutex unnecessarily — pick one cadence per broker).
func RunSweeper(ctx context.Context, cfg SweeperConfig) error {
	if cfg.Broker == nil {
		return fmt.Errorf("sweeper: Broker required")
	}
	if cfg.ActiveIDsFn == nil {
		return fmt.Errorf("sweeper: ActiveIDsFn required")
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	tick := func() {
		swept, err := cfg.Broker.SweepOrphans(ctx, cfg.ActiveIDsFn())
		if err != nil && cfg.OnSweepError != nil {
			cfg.OnSweepError(err)
		}
		if cfg.OnSweepResult != nil {
			cfg.OnSweepResult(swept)
		}
	}

	// Run one sweep immediately so a fresh start cleans up anything
	// the previous instance leaked.
	tick()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			tick()
		}
	}
}

// _ keeps io referenced for the Audit-via-discard convenience
// callers occasionally use. Removing this when callers stop needing it.
var _ = io.Discard
