// Daemon wiring for the housekeeping repairs: the one constructor the daemon
// start-up path calls, and the Coordinator hook that runs the Repairer on the
// supervisor loop's schedule (the idle tick, right after the loop's steps).
package autonomous

import (
	"context"

	"github.com/dmz006/datawatch/internal/config"
)

// HousekeepingWiring is what the daemon supplies to build the Repairer. Every
// Ops field is an optional override: nil takes the production default
// (the Automaton-store-backed ops, or the supervisor-step repair for the
// lane_fill and leftovers groups). Tests pass fakes.
type HousekeepingWiring struct {
	// API backs the default task chain and advance ops.
	API *API
	// Steps backs the default lane_fill and leftovers repairs.
	Steps *SupervisorLoopSteps
	// Settings reads autonomous.housekeeping_repairs live.
	Settings func() config.HousekeepingRepairsConfig
	// Log writes one decision-log line per repair.
	Log func(line string)
	// Alert raises one alert.
	Alert func(level, title, body string)
	// StorePath persists the bounded record; empty keeps it in memory.
	StorePath string

	Chain     TaskChainOps
	Advance   AdvanceOps
	LaneFill  LaneFillOps
	Leftovers LeftoverReleaseOps
}

// NewHousekeepingRepairer builds the Repairer with the repairs of all four
// groups from the wiring.
func NewHousekeepingRepairer(w HousekeepingWiring) *Repairer {
	var repairs []Repair
	chain := w.Chain
	if chain == nil && w.API != nil {
		chain = ManagerChainOps{API: w.API}
	}
	repairs = append(repairs, NewTaskChainRepairs(chain)...)
	adv := w.Advance
	if adv == nil && w.API != nil {
		adv = ManagerAdvanceOps{API: w.API}
	}
	repairs = append(repairs, NewAdvanceJudgeRepairs(adv)...)
	if w.LaneFill != nil {
		repairs = append(repairs, NewLaneFillRepairs(w.LaneFill)...)
	}
	if w.Leftovers != nil {
		repairs = append(repairs, NewLeftoverReleaseRepairs(w.Leftovers)...)
	}
	for _, r := range NewSupervisorStepRepairs(w.Steps) {
		switch r.Group() {
		case RepairGroupLaneFill:
			if w.LaneFill == nil {
				repairs = append(repairs, r)
			}
		case RepairGroupLeftovers:
			if w.Leftovers == nil {
				repairs = append(repairs, r)
			}
		}
	}
	return NewRepairer(RepairerHooks{Settings: w.Settings, Log: w.Log, Alert: w.Alert, StorePath: w.StorePath}, repairs...)
}

// SetHousekeepingRepairer registers the Repairer the idle tick runs. Nil turns
// the pass off.
func (c *Coordinator) SetHousekeepingRepairer(r *Repairer) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.hkRepairer = r
	c.mu.Unlock()
}

// HousekeepingPass makes one pass of the registered Repairer and returns the
// records it added. The idle tick calls it after the supervisor loop's pass.
func (c *Coordinator) HousekeepingPass(ctx context.Context) []RepairRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	r := c.hkRepairer
	c.mu.Unlock()
	return r.Run(ctx)
}
