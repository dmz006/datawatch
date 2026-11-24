// Planning the engine starts by itself: lane fill decompose, the planner
// restart after a daemon restart, and the re-plan after a reworded spec. These
// starts are daemon-enforced here, with no model in the loop:
//   - they wait while the operator has paused the project (pause_gate.go);
//   - a plan that was accepted is never planned again by the engine;
//   - an Automaton whose plans keep failing is held after autoPlanStartLimit
//     engine starts, with one alert, instead of spawning planner sessions for
//     ever. An operator-run decompose is never gated or counted.

package autonomous

import (
	"context"
	"fmt"
	"log"
)

// autoPlanStartLimit is how many planner starts the engine makes by itself for
// one Automaton, without an accepted plan in between, before it holds the
// Automaton. The hold resets the count, so a release gives a fresh set.
const autoPlanStartLimit = 3

// planningGateActor is the actor named on the hold the gate places.
const planningGateActor = "planning-gate"

// planningHoldKeyPrefix names the cancel slot of a restarted planner that waits
// for a pause to end, so WaitIdle ends the wait on shutdown.
const planningHoldKeyPrefix = "planning-hold:"

// DecomposeAutomatic is Decompose for a start the engine makes by itself: it
// runs the planning gate first and counts the start. The operator's own
// decompose (REST, CLI, chat, the PWA) calls Decompose directly.
func (m *Manager) DecomposeAutomatic(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.gateAutomaticPlanning(prd); err != nil {
		return nil, err
	}
	return m.Decompose(prdID)
}

// gateAutomaticPlanning decides whether the engine may start planning prd now
// and, when it may, counts the start. It returns the refusal otherwise.
func (m *Manager) gateAutomaticPlanning(prd *PRD) error {
	if prd == nil {
		return nil
	}
	if err := m.refuseHeld(prd); err != nil {
		return err
	}
	var dir string
	var status PRDStatus
	m.store.WithLock(func() { dir, status = prd.ProjectDir, NormalizePRDStatus(prd.Status) })
	if b, blocked := m.pauseBlock(dir); blocked {
		return fmt.Errorf("automaton %q is not planned: %s", prd.ID, b.Message())
	}
	if status != PRDDraft && status != PRDPlanning {
		return fmt.Errorf("automaton %q already has a plan (status %s); the engine does not plan it again, the operator can", prd.ID, status)
	}
	var starts int
	m.store.WithLock(func() { starts = prd.AutoPlanStarts })
	if starts >= autoPlanStartLimit {
		return m.holdForPlanningLoop(prd, starts)
	}
	m.store.MutatePRD(prd.ID, func(p *PRD) bool { p.AutoPlanStarts++; return true })
	return nil
}

// holdForPlanningLoop holds prd after the engine started planning it starts
// times without an accepted plan, resets the count and raises one alert.
func (m *Manager) holdForPlanningLoop(prd *PRD, starts int) error {
	reason := fmt.Sprintf("the engine started planning %d times without an accepted plan; plan it by hand, or release the hold to let the engine try again", starts)
	m.store.MutatePRD(prd.ID, func(p *PRD) bool { p.AutoPlanStarts = 0; return true })
	if _, err := m.Hold(prd.ID, HoldRequest{Reason: reason, Condition: "the operator plans it or releases the hold"}, planningGateActor); err != nil {
		log.Printf("[autonomous] prd=%s planning loop: could not hold: %v", prd.ID, err)
		return fmt.Errorf("automaton %q is not planned again: %s (%v)", prd.ID, reason, err)
	}
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert != nil {
		alert("error", "Automaton held: planning kept failing",
			fmt.Sprintf("Automaton: %s (%s)\n%s.", prd.ID, prd.Title, reason))
	}
	return fmt.Errorf("automaton %q is held: %s", prd.ID, reason)
}

// waitPlanningUnpaused blocks until prd's project is no longer paused. It
// reports false when ctx ends or the Automaton is gone or no longer waiting to
// be planned, so the restart is dropped.
func (m *Manager) waitPlanningUnpaused(ctx context.Context, prdID string) bool {
	poll := m.pauseWaitPoll()
	clock := m.spawnWaitClock()
	for {
		prd, ok := m.store.GetPRD(prdID)
		if !ok {
			return false
		}
		var dir string
		var status PRDStatus
		m.store.WithLock(func() { dir, status = prd.ProjectDir, NormalizePRDStatus(prd.Status) })
		if status != PRDPlanning && status != PRDDraft {
			return false
		}
		if _, blocked := m.pauseBlock(dir); !blocked {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-clock.After(poll):
		}
	}
}

// startPlanningAfterPause starts planning for prdID again in the background
// once the operator's pause (if any) has ended. The caller holds one execWG
// count for the goroutine. The wait is cancelled by WaitIdle.
func (a *API) startPlanningAfterPause(prdID string) {
	ctx, cancel := context.WithCancel(context.Background())
	key := planningHoldKeyPrefix + prdID
	a.runMu.Lock()
	if a.runCancels == nil {
		a.runCancels = map[string]context.CancelFunc{}
	}
	a.runCancels[key] = cancel
	a.runMu.Unlock()
	go func() {
		defer a.execWG.Done()
		defer func() {
			cancel()
			a.runMu.Lock()
			delete(a.runCancels, key)
			a.runMu.Unlock()
		}()
		if !a.M.waitPlanningUnpaused(ctx, prdID) {
			return
		}
		if _, err := a.M.DecomposeAutomatic(prdID); err != nil {
			fmt.Printf("[autonomous] boot-resume: prd=%s planning restart failed: %v\n", prdID, err)
		}
	}()
}
