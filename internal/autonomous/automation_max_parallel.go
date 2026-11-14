// Automation max_parallel: a cap on how many members run at once.
//
// Automation.MaxParallel caps the running members of the whole Automation, and a
// fork member's MaxParallel caps the running members of that fork's branches. 0
// (or unset) means no limit, which is the behavior before the field existed. A
// member that would start while its scope is full is not started and not
// blocked: it holds its cursor, flagged WaitingForSlot, and the stepper starts
// it in the same step that frees a slot, in member order (the order of
// Automation.Members). The flag and the held cursor are stored with the
// Automation, so boot reconcile finds a waiting member exactly where it was and
// starts it through the same admission check (automation_reconcile.go).
//
// Everything here is a pure read of the Automation; the stepper
// (automation_advance.go) and the reconcile path call it.

package autonomous

import (
	"fmt"
	"sort"
)

// SlotWaitReason is the plain-language reason a member that holds its cursor
// without running reports in the status data. It is never reported as blocked.
const SlotWaitReason = "waiting for a free slot"

// startsWork reports whether m is a member that runs something and so takes a
// slot: a PRD member or a nested Automation. A fork fires at once, a join only
// waits, and a pending item runs nothing.
func startsWork(m *AutomationMember) bool {
	k := m.kind()
	return k == MemberKindPRD || k == MemberKindAutomation
}

// occupiesSlot reports whether m is running work that counts against a limit.
func occupiesSlot(m *AutomationMember) bool {
	return startsWork(m) && m.Status == MemberRunning
}

// memberWaitingForSlot reports whether m holds its cursor without having
// started because a max_parallel limit was reached. A member that has started
// or ended is never waiting, whatever its stored flag says.
func memberWaitingForSlot(m *AutomationMember) bool {
	return m != nil && m.WaitingForSlot && startsWork(m) && memberUnstarted(m)
}

// effectiveMaxParallel returns the limit that applies to the members of fork:
// the smaller of the Automation's limit and the fork's limit, ignoring a limit
// that is unset. fork may be nil for a member outside any fork. 0 means no
// limit applies.
func effectiveMaxParallel(a *Automation, fork *AutomationMember) int {
	limit := a.MaxParallel
	if fork != nil && fork.MaxParallel > 0 && (limit == 0 || fork.MaxParallel < limit) {
		limit = fork.MaxParallel
	}
	if limit < 0 {
		return 0
	}
	return limit
}

// anyMaxParallel reports whether a or any of its forks carries a limit, so the
// common unlimited case skips every other check.
func anyMaxParallel(a *Automation) bool {
	if a.MaxParallel > 0 {
		return true
	}
	for i := range a.Members {
		if a.Members[i].kind() == MemberKindFork && a.Members[i].MaxParallel > 0 {
			return true
		}
	}
	return false
}

// memberIndex maps each member ID to its position in a.Members, the order the
// waiting members are started in.
func memberIndex(a *Automation) map[string]int {
	idx := make(map[string]int, len(a.Members))
	for i := range a.Members {
		idx[a.Members[i].ID] = i
	}
	return idx
}

// byMemberOrder returns a copy of ids sorted by member order; an ID that is not
// a member sorts last, then by ID, so the result is always deterministic.
func byMemberOrder(a *Automation, ids []string) []string {
	idx := memberIndex(a)
	out := append([]string(nil), ids...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, oki := idx[out[i]]
		pj, okj := idx[out[j]]
		switch {
		case oki && okj:
			return pi < pj
		case oki != okj:
			return oki
		}
		return out[i] < out[j]
	})
	return out
}

// forkOfMember returns the fork whose branch chains hold member id, or nil.
func forkOfMember(a *Automation, id string) *AutomationMember {
	fork, _, _ := branchChainOf(a, id)
	return fork
}

// forkRunning counts the running members on fork's branch chains.
func forkRunning(a *Automation, fork *AutomationMember) int {
	n := 0
	for _, chain := range forkChains(a, fork) {
		for _, id := range chain {
			if m := automationMember(a, id); m != nil && occupiesSlot(m) {
				n++
			}
		}
	}
	return n
}

// runningMembers counts the running members of the whole Automation.
func runningMembers(a *Automation) int {
	n := 0
	for i := range a.Members {
		if occupiesSlot(&a.Members[i]) {
			n++
		}
	}
	return n
}

// slotWaiters returns the IDs of the members that hold a cursor and wait for a
// free slot, in member order.
func slotWaiters(a *Automation) []string {
	var out []string
	for i := range a.Members {
		m := &a.Members[i]
		if memberWaitingForSlot(m) && isLiveCursor(a, m.ID) {
			out = append(out, m.ID)
		}
	}
	return out
}

// parallelAdmit decides which of the candidate members may start now. The
// candidates are taken in member order and each is admitted while its scopes
// have room: the Automation's running count must be under its limit, and, for a
// fork branch member, the fork's running count under the fork's limit. An
// admitted candidate counts as running for the ones behind it, so the first
// members in order take the free slots. A candidate whose scope is full is not
// admitted, and a later candidate in another scope is judged on its own. With no
// limit anywhere every candidate is admitted.
func parallelAdmit(a *Automation, candidates []string) map[string]bool {
	admit := make(map[string]bool, len(candidates))
	if !anyMaxParallel(a) {
		for _, id := range candidates {
			admit[id] = true
		}
		return admit
	}
	total := runningMembers(a)
	perFork := make(map[string]int)
	for _, id := range byMemberOrder(a, candidates) {
		fork := forkOfMember(a, id)
		if a.MaxParallel > 0 && total >= a.MaxParallel {
			continue
		}
		if fork != nil && fork.MaxParallel > 0 {
			n, seen := perFork[fork.ID]
			if !seen {
				n = forkRunning(a, fork)
			}
			if n >= fork.MaxParallel {
				perFork[fork.ID] = n
				continue
			}
			perFork[fork.ID] = n + 1
		}
		total++
		admit[id] = true
	}
	return admit
}

// slotAdmitted reports whether member id may start now, in line behind the
// members already waiting for a slot. It is the single check the stepper and
// the reconcile path share, so a member starts on the same terms either way.
func slotAdmitted(a *Automation, id string) bool {
	if !anyMaxParallel(a) {
		return true
	}
	m := automationMember(a, id)
	if m == nil || !startsWork(m) {
		return true
	}
	cands := []string{id}
	for _, w := range slotWaiters(a) {
		if w != id {
			cands = append(cands, w)
		}
	}
	return parallelAdmit(a, cands)[id]
}

// holdForSlot is the stepper's gate on a member start: when the member's scope
// is full it keeps the cursor on the member, flags it, records why and reports
// true, and the caller starts nothing. The Automation is not blocked and not
// idle: the members that fill the slots are running.
func (st *stepper) holdForSlot(next *AutomationMember) bool {
	if !startsWork(next) || slotAdmitted(st.a, next.ID) {
		return false
	}
	st.waitForSlot(next)
	return true
}

// waitForSlot records next as waiting for a free slot with its cursor held.
func (st *stepper) waitForSlot(next *AutomationMember) {
	already := memberWaitingForSlot(next) && isLiveCursor(st.a, next.ID)
	next.WaitingForSlot = true
	if !isLiveCursor(st.a, next.ID) {
		addCursor(st.a, next.ID)
	}
	if already {
		return
	}
	limit := effectiveMaxParallel(st.a, forkOfMember(st.a, next.ID))
	name := st.name(next)
	st.decision("waiting", fmt.Sprintf("member %s %s (max_parallel %d)", name, SlotWaitReason, limit))
	st.audit("automation_waiting_for_slot", map[string]any{
		"automation_id": st.a.ID, "member": next.ID, "prd_id": next.PRDID,
		"max_parallel": limit, "running": runningMembers(st.a), "actor": coordinatorActor,
	})
}

// promoteWaiting starts the members waiting for a free slot that a finished
// member just released, in member order. It runs at the end of every step, so
// the release and the start are one write and a restart cannot fall between
// them. It starts nothing while the Automation is paused, blocked or stopped by
// the budget gate; those members wait on the cursor until the Automation moves.
func (st *stepper) promoteWaiting() {
	a := st.a
	if (a.Status != AutomationActive && a.Status != AutomationDraft) || st.eff.blocked != "" || st.eff.completed || st.eff.budget != nil {
		return
	}
	saved := st.advNote
	defer func() { st.advNote = saved }()
	// Start the first admitted waiter, then judge again: a start that fails its
	// gate takes no slot, so the next waiter in order may now be admitted. Each
	// pass clears one waiter, so the loop ends.
	for range a.Members {
		waiters := slotWaiters(a)
		if len(waiters) == 0 {
			return
		}
		admit := parallelAdmit(a, waiters)
		var m *AutomationMember
		for _, id := range byMemberOrder(a, waiters) {
			if admit[id] {
				m = automationMember(a, id)
				break
			}
		}
		if m == nil {
			return
		}
		st.advNote = fmt.Sprintf("%s started: a slot is free", st.name(m))
		dropCursor(a, m.ID)
		st.enterAdmitted(m)
		if st.eff.blocked != "" || st.eff.budget != nil || st.eff.completed {
			return
		}
	}
}
