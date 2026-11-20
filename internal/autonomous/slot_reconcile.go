package autonomous

import (
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// PiPromptPool is the ledger pool whose held slots the orphan reconcile walks.
const PiPromptPool = "llm:pi-prompt"

// Reasons recorded on a freed slot.
const (
	SlotReasonNoSessionNoLease  = "no live session and no node lease"
	SlotReasonAutomatonFinished = "Automaton finished"
	judgeHolderPrefix           = "judge:"
	verifyHolderPrefix          = "verify:"
)

// SlotLedgerView is the slice of the ledger the reconcile needs. The real
// *capacity.Ledger satisfies it; tests use a fake.
type SlotLedgerView interface {
	// Leases lists every held lease.
	Leases() []capacity.Lease
	// ReleaseIf frees the holder's lease only if it is still the one acquired
	// at the given time, and reports whether it freed one.
	ReleaseIf(holder string, acquired time.Time) bool
}

// SessionLiveness reports whether a session id is running.
type SessionLiveness interface {
	SessionLive(sessionID string) bool
}

// NodeLeaseCheck reports whether the holder has a node lease.
type NodeLeaseCheck interface {
	HasNodeLease(ls capacity.Lease) bool
}

// AutomatonFinishedCheck reports whether the unit's Automaton is in a
// terminal state (merged, cancelled or failed).
type AutomatonFinishedCheck interface {
	AutomatonFinished(unitID string) bool
}

// FreedSlot is one released slot.
type FreedSlot struct {
	Holder string `json:"holder"`
	Pool   string `json:"pool"`
	Reason string `json:"reason"`
}

// ReconcileOrphanSlots frees held pi-prompt pool slots of judge and verifier
// holders that no longer have a reason to hold them: no live session and no
// node lease, or an Automaton that is finished with no live session. A holder
// with a live session is never freed, whatever its lease state.
//
// It NEVER ends or kills a session: it only releases ledger slots and calls
// no session-ending function. The ledger lease list is copied first and every
// check runs without the ledger lock (the same rule as Ledger.Reap); the
// release re-checks the lease identity (Acquired), so a lease replaced while
// the checks ran is left alone. Running it twice frees nothing the second time.
func ReconcileOrphanSlots(led SlotLedgerView, sessions SessionLiveness, nodes NodeLeaseCheck, done AutomatonFinishedCheck) []FreedSlot {
	if led == nil {
		return nil
	}
	var freed []FreedSlot
	for _, ls := range led.Leases() {
		if !holdsPool(ls, PiPromptPool) {
			continue
		}
		unit, ok := slotUnit(ls.Holder)
		if !ok {
			continue
		}
		if ls.SessionID != "" && sessions != nil && sessions.SessionLive(ls.SessionID) {
			continue
		}
		reason := ""
		switch {
		case done != nil && done.AutomatonFinished(unit):
			reason = SlotReasonAutomatonFinished
		case nodes != nil && !nodes.HasNodeLease(ls):
			reason = SlotReasonNoSessionNoLease
		}
		if reason == "" {
			continue
		}
		if led.ReleaseIf(ls.Holder, ls.Acquired) {
			freed = append(freed, FreedSlot{Holder: ls.Holder, Pool: PiPromptPool, Reason: reason})
		}
	}
	sort.Slice(freed, func(i, j int) bool { return freed[i].Holder < freed[j].Holder })
	return freed
}

func holdsPool(ls capacity.Lease, pool string) bool {
	for _, p := range ls.Pools {
		if p == pool {
			return true
		}
	}
	return false
}

// slotUnit returns the unit id of a judge or verifier holder.
func slotUnit(holder string) (string, bool) {
	if u, ok := strings.CutPrefix(holder, judgeHolderPrefix); ok && u != "" {
		return u, true
	}
	if u, ok := strings.CutPrefix(holder, verifyHolderPrefix); ok && u != "" {
		return u, true
	}
	return "", false
}
