// Planning the engine starts by itself: lane fill decompose, the planner
// restart after a daemon restart, and the re-plan after a reworded spec. These
// starts are daemon-enforced here, with no model in the loop:
//   - they wait while the operator has paused the project (pause_gate.go),
//     including each further planner session of a start already running;
//     planning of a child or scan-fix Automaton waits too (it does not fail);
//   - a plan that was accepted is never planned again by the engine;
//   - an Automaton whose plans keep failing is held after autoPlanStartLimit
//     engine starts, with one alert, instead of spawning planner sessions for
//     ever. An operator-run decompose is never gated or counted, and its
//     planner skips the automatic-only capacity blocks (usage back-off).

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

// planOrigins counts the planning calls in flight per Automaton by who started
// them. The zero value is ready to use.
type planOrigins struct {
	mu sync.Mutex
	// engine counts starts that passed the planning gate.
	engine map[string]int
	// operator counts calls from an operator surface (REST, and through it the
	// CLI, MCP, chat and the web app) that did not pass the gate.
	operator map[string]int
}

func (o *planOrigins) add(m *map[string]int, id string, n int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if *m == nil {
		*m = map[string]int{}
	}
	if (*m)[id] += n; (*m)[id] <= 0 {
		delete(*m, id)
	}
}

// endEnginePlan ends the engine start that gateAutomaticPlanning admitted.
func (m *Manager) endEnginePlan(id string) {
	m.planOrigins.add(&m.planOrigins.engine, id, -1)
}

func (m *Manager) enginePlanning(id string) bool {
	m.planOrigins.mu.Lock()
	defer m.planOrigins.mu.Unlock()
	return m.planOrigins.engine[id] > 0
}

// noteOperatorPlan marks a planning call an operator surface makes, unless the
// engine is the one calling (lane fill uses the same entry). Call the returned
// function when the planning call returns.
func (m *Manager) noteOperatorPlan(id string) func() {
	if m.enginePlanning(id) {
		return func() {}
	}
	m.planOrigins.add(&m.planOrigins.operator, id, 1)
	return func() { m.planOrigins.add(&m.planOrigins.operator, id, -1) }
}

// operatorStartedPlan reports whether the planning in flight for id was started
// by the operator. A start whose origin is not known counts as automatic, and
// so does one that overlaps an engine start.
func (m *Manager) operatorStartedPlan(id string) bool {
	m.planOrigins.mu.Lock()
	defer m.planOrigins.mu.Unlock()
	return m.planOrigins.operator[id] > 0 && m.planOrigins.engine[id] == 0
}

// planningPausedError is the gate's refusal of a paused project.
type planningPausedError struct{ msg string }

func (e *planningPausedError) Error() string { return e.msg }

// planEngineOpened plans an Automaton the engine opened itself (a child or a
// scan-fix task) through the planning gate. A paused project makes it wait, not
// fail: wait blocks while the given project directory is paused.
func (m *Manager) planEngineOpened(id string, wait func(dir string) error) error {
	for {
		if err := wait(m.planDirOf(id)); err != nil {
			return err
		}
		_, err := m.DecomposeAutomatic(id)
		var paused *planningPausedError
		if errors.As(err, &paused) {
			continue // paused between the wait and the gate
		}
		return err
	}
}

// runEngineOpened runs an Automaton the engine opened itself, waiting while its
// project is paused instead of taking the pause refusal as a failed run.
func (m *Manager) runEngineOpened(id string, wait func(dir string) error, run func() error) error {
	for {
		dir := m.planDirOf(id)
		if err := wait(dir); err != nil {
			return err
		}
		err := run()
		if err != nil {
			// Only a run the pause refused is tried again: that Automaton
			// never started. A run that started keeps its own error.
			if _, blocked := m.pauseBlock(dir); blocked && m.automatonNotStarted(id) {
				continue
			}
		}
		return err
	}
}

func (m *Manager) planDirOf(id string) string {
	var dir string
	if prd, ok := m.store.GetPRD(id); ok {
		m.store.WithLock(func() { dir = prd.ProjectDir })
	}
	return dir
}

func (m *Manager) automatonNotStarted(id string) bool {
	st, ok := m.prdStatusOf(id)
	return ok && (st == PRDApproved || st == PRDActive)
}

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
	defer m.endEnginePlan(prdID)
	return m.Decompose(prdID)
}

// gateAutomaticPlanning decides whether the engine may start planning prd now
// and, when it may, counts the start and marks it as the engine's: the caller
// must call endEnginePlan when the planning call returns. It returns the
// refusal otherwise.
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
		return &planningPausedError{fmt.Sprintf("automaton %q is not planned: %s", prd.ID, b.Message())}
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
	m.planOrigins.add(&m.planOrigins.engine, prd.ID, 1)
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
