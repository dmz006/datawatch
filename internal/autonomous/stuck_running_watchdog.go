package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// SetStuckRunningHooks wires the stuck-running watchdog: refire re-launches the
// completion rollup of a running PRD the way boot-resume does, and execLive
// reports whether a live executor already holds the PRD (nil = never live).
// Without a refire hook, or without a session lister, the watchdog is a no-op.
func (m *Manager) SetStuckRunningHooks(refire func(prdID string) (bool, error), execLive func(prdID string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stuckRefireFn = refire
	m.stuckExecLiveFn = execLive
}

// stuckRunningCaller names the watchdog in the audit trail.
const stuckRunningCaller = "autonomous.stuck-running"

// sweepStuckRunning is one stuck-running watchdog pass, modelled on the orphan
// sweep. A PRD is stuck when its status is running, every task is terminal,
// no live autonomous session (worker, verifier, guardrail, decompose ...)
// belongs to it and no executor holds its run slot. The first pass that sees a
// PRD stuck records a first-seen time; every pass where any condition stops
// holding clears it. Design: re-fire the completion rollup once, then block.
// After one full bound the rollup is re-fired once (Decision
// stuck_running_refire) and the timer restarts; if the PRD is still stuck after
// a second full bound it moves to blocked through commitRollupStatus, so a
// concurrent writer wins, with a Decision (stuck_running_blocked), an audit
// record and the PRD status event. The bound is read live each pass.
func (m *Manager) sweepStuckRunning() {
	m.mu.Lock()
	listFn, refireFn, liveFn, auditFn := m.sessionListerFn, m.stuckRefireFn, m.stuckExecLiveFn, m.auditFn
	bound := time.Duration(config.NormalizeStuckRunningSeconds(m.cfg.StuckRunningSeconds)) * time.Second
	m.mu.Unlock()
	if listFn == nil || refireFn == nil {
		return
	}

	activePRD := map[string]bool{}
	for _, s := range listFn() {
		if s.Alive && s.PRDID != "" && strings.HasPrefix(s.Name, workerSessionNamePrefix) {
			activePRD[s.PRDID] = true
		}
	}

	now := m.orphanClock()
	known := map[string]bool{}
	for _, prd := range m.store.ListPRDs() {
		id := prd.ID
		known[id] = true
		// Revision first: a write landing after this read makes the block
		// commit stale instead of being overwritten.
		rev, _ := m.store.PRDRev(id)
		m.mu.Lock()
		running := prd.Status == PRDRunning
		m.mu.Unlock()
		terminal := false
		if running {
			m.readLocked(func() { terminal = prdAllTasksTerminal(prd) })
		}
		active := activePRD[id] || (liveFn != nil && liveFn(id))

		m.mu.Lock()
		if m.stuckSeen == nil {
			m.stuckSeen = map[string]time.Time{}
		}
		if m.stuckRefired == nil {
			m.stuckRefired = map[string]bool{}
		}
		if !running || !terminal {
			// Not stuck, and the earlier re-fire (if any) no longer applies.
			delete(m.stuckSeen, id)
			delete(m.stuckRefired, id)
			m.mu.Unlock()
			continue
		}
		if active {
			delete(m.stuckSeen, id)
			m.mu.Unlock()
			continue
		}
		first, seen := m.stuckSeen[id]
		if !seen {
			m.stuckSeen[id] = now
			m.mu.Unlock()
			continue
		}
		age := now.Sub(first)
		if age <= bound {
			m.mu.Unlock()
			continue
		}
		refired := m.stuckRefired[id]
		if !refired {
			m.stuckRefired[id] = true
		}
		m.stuckSeen[id] = now // restart the timer
		m.mu.Unlock()

		if !refired {
			note := fmt.Sprintf("running with every task terminal, no live session or executor for %s; re-firing the completion rollup once, then blocking if still stuck after another %s",
				age.Round(time.Second), bound)
			log.Printf("[autonomous] stuck-running: prd=%s: %s", id, note)
			m.recordStuckDecision(id, "stuck_running_refire", note)
			started, err := refireFn(id)
			if err != nil {
				log.Printf("[autonomous] stuck-running: prd=%s refire: %v", id, err)
			} else if !started {
				log.Printf("[autonomous] stuck-running: prd=%s refire did not start an executor", id)
			}
			continue
		}

		reason := fmt.Sprintf("stuck running: every task terminal and no live session or executor for %s after the completion rollup was re-fired once", age.Round(time.Second))
		old, ok := m.commitRollupStatus(prd, PRDRunning, rev, PRDBlocked, "stuck_running", reason)
		m.mu.Lock()
		delete(m.stuckSeen, id)
		delete(m.stuckRefired, id)
		m.mu.Unlock()
		if !ok {
			continue
		}
		log.Printf("[autonomous] stuck-running: prd=%s blocked: %s", id, reason)
		m.recordStuckDecision(id, "stuck_running_blocked", reason)
		if auditFn != nil {
			auditFn("autonomous_stuck_running_blocked", map[string]any{
				"prd_id": id, "caller": stuckRunningCaller, "stuck_seconds": int(age.Seconds()),
				"bound_seconds": int(bound.Seconds()), "old_status": string(old), "new_status": string(PRDBlocked),
			})
		}
		m.emitPRDStatus(prd, old, lastDecision(prd, "stuck_running_blocked"), nil)
	}

	m.mu.Lock()
	for id := range m.stuckSeen {
		if !known[id] {
			delete(m.stuckSeen, id)
		}
	}
	for id := range m.stuckRefired {
		if !known[id] {
			delete(m.stuckRefired, id)
		}
	}
	m.mu.Unlock()
}

// recordStuckDecision appends a Decision to the PRD, re-fetched from the store,
// and saves it. It never touches Status.
func (m *Manager) recordStuckDecision(prdID, kind, note string) {
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	at := m.orphanClock()
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{At: at, Kind: kind, Actor: "autonomous", Note: note})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] stuck-running: save prd %s: %v", prdID, err)
	}
}
