package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// sessionLiveFn adapts the session manager's running check to
// autonomous.SessionLiveness.
type sessionLiveFn func(id string) bool

func (f sessionLiveFn) SessionLive(id string) bool { return f(id) }

// ledgerNodeLease reads the node lease from the lease itself: a lease that
// holds a node pool (node:<name>) still has its node lease.
type ledgerNodeLease struct {
	now func() time.Time
	// grace is how long a fresh lease counts as having a node lease, so a
	// holder that was just admitted and has not registered its session yet is
	// never freed. It comes from autonomous.capacity_slot_reconcile_grace_seconds.
	grace time.Duration
}

func (n ledgerNodeLease) HasNodeLease(ls capacity.Lease) bool {
	if n.now != nil && n.now().Sub(ls.Acquired) < n.grace {
		return true
	}
	for _, p := range ls.Pools {
		if strings.HasPrefix(p, "node:") || strings.HasPrefix(p, "node-verify:") {
			return true
		}
	}
	return false
}

// automatonFinishedFn adapts a lookup to autonomous.AutomatonFinishedCheck.
type automatonFinishedFn func(unitID string) bool

func (f automatonFinishedFn) AutomatonFinished(id string) bool { return f(id) }

// managerAutomatonFinished reports whether the unit (a task or an Automaton)
// is in a terminal state in the autonomous manager's store. An unknown id is
// not finished.
func managerAutomatonFinished(amgr *autonomous.Manager) automatonFinishedFn {
	return func(unitID string) bool {
		if amgr == nil || amgr.Store() == nil {
			return false
		}
		st := amgr.Store()
		if t, ok := st.GetTask(unitID); ok {
			var status autonomous.TaskStatus
			st.WithLock(func() { status = t.Status })
			switch status {
			case autonomous.TaskCompleted, autonomous.TaskFailed, autonomous.TaskCancelled:
				return true
			}
			return false
		}
		if p, ok := st.GetPRD(unitID); ok {
			var status autonomous.PRDStatus
			st.WithLock(func() { status = p.Status })
			switch status {
			case autonomous.PRDCompleted, autonomous.PRDCancelled, autonomous.PRDArchived, autonomous.PRDFailed:
				return true
			}
		}
		return false
	}
}

// slotReconcileDeps is everything the periodic slot reconcile needs; tests
// fill it with fakes.
type slotReconcileDeps struct {
	Ledger   autonomous.SlotLedgerView
	Sessions autonomous.SessionLiveness
	Nodes    autonomous.NodeLeaseCheck
	Done     autonomous.AutomatonFinishedCheck
	// HolderName renders a holder id as plain words with the short id second.
	HolderName func(holder string) string
	Alert      func(title, body string)
	Logf       func(format string, args ...any)
	// Count is called once per freed slot (the reaped-lease metric).
	Count func()
}

// runSlotReconcile frees orphan pi-prompt slots, journals one line and raises
// one alert per freed slot. It releases ledger slots only and never ends a
// session.
func runSlotReconcile(d slotReconcileDeps) []autonomous.FreedSlot {
	freed := autonomous.ReconcileOrphanSlots(d.Ledger, d.Sessions, d.Nodes, d.Done)
	for _, f := range freed {
		name := f.Holder
		if d.HolderName != nil {
			if n := d.HolderName(f.Holder); n != "" {
				name = n
			}
		}
		if d.Logf != nil {
			d.Logf("[capacity] reaped leaked lease holder=%s pool=%s: %s\n", f.Holder, f.Pool, f.Reason)
		}
		if d.Alert != nil {
			d.Alert("Capacity slot freed", fmt.Sprintf("Freed a %s slot held by %s: %s", slotPoolLabel(f.Pool), name, f.Reason))
		}
		if d.Count != nil {
			d.Count()
		}
	}
	return freed
}

// slotPoolLabel renders a ledger pool id in plain words for alert text.
func slotPoolLabel(pool string) string {
	if pool == autonomous.PiPromptPool {
		return "pi-prompt worker (llm:pi-prompt)"
	}
	return pool
}
