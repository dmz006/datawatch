// Pending item hold: what the Automation advance does when the cursor reaches
// a pending item member. A pending item is named work with no Automaton yet,
// so there is nothing to start. A waiting item holds the Automation at the
// cursor (the Automation blocks with a reason that names the item) until an
// Automaton is attached; the skip policy passes it by, and a done item
// releases the hold. The same decision runs when the cursor is already on the
// member: a resume, a restart reconcile and an activation all re-check it.

package autonomous

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
)

// Outcomes recorded on a pending item member that is passed over.
const (
	pendingOutcomeSkipped = "skipped_pending_item"
	pendingOutcomeDone    = "pending_item_done"
)

// pendingFacts is a pending item member's registry state, read once into the
// step snapshot so the step stays a pure function of its inputs.
type pendingFacts struct {
	id      string
	title   string
	ref     string
	state   PendingItemState
	missing bool   // the registry has no such item
	policy  string // config.PendingItemPolicyHold or config.PendingItemPolicySkip
}

// pendingAction is what the advance does with a pending item member.
type pendingAction int

const (
	pendingHold pendingAction = iota // keep the cursor here and block
	pendingSkip                      // pass the member on its on_complete edge
	pendingDone                      // the work is done: pass it on its on_complete edge
)

// label names the item in messages: its title, else its ID.
func (f *pendingFacts) label() string {
	if f.title != "" {
		return f.title
	}
	return f.id
}

// decide picks the action for the item and, for a hold, the reason to show.
func (f *pendingFacts) decide() (pendingAction, string) {
	switch {
	case f.missing:
		return pendingHold, fmt.Sprintf("member %q is a pending item that is not in the registry (item %s); remove the member or restore the item to continue", f.label(), f.id)
	case f.state == PendingItemDone:
		return pendingDone, ""
	case f.state == PendingItemAttached:
		return pendingHold, fmt.Sprintf("member %q is a pending item that has an Automaton attached but is not converted yet (item %s); resume the Automation once the attach is complete", f.label(), f.id)
	case f.policy == config.PendingItemPolicySkip:
		return pendingSkip, ""
	}
	return pendingHold, fmt.Sprintf("member %q is a pending item waiting for an Automaton to be attached (item %s); attach one to continue", f.label(), f.id)
}

// pendingMemberInfo reads a pending item member's snapshot entry: the item's
// state, title and ref from the registry, and the policy in effect.
func (c *Coordinator) pendingMemberInfo(m AutomationMember, policy string) memberInfo {
	f := &pendingFacts{id: m.PendingItemID, policy: policy}
	name := "pending item " + m.PendingItemID
	if item, ok := c.m.pendingItemByID(m.PendingItemID); ok {
		f.title, f.ref, f.state = item.Title, item.Ref, item.State
		if item.Title != "" {
			name = item.Title
		}
	} else {
		f.missing = true
	}
	return memberInfo{name: name, pending: f}
}

// enterPending handles the cursor arriving on a pending item member. The
// Automation is not paused here: enter() has already moved the cursor and
// started nothing for a paused one.
func (st *stepper) enterPending(next *AutomationMember) {
	f := st.snap[next.ID].pending
	if f == nil {
		f = &pendingFacts{id: next.PendingItemID, missing: true}
	}
	act, reason := f.decide()
	switch act {
	case pendingSkip:
		st.passPending(next, f, endOutcome{
			edge: edgeOnComplete, memStatus: MemberSkipped, outcome: pendingOutcomeSkipped, success: true,
		}, fmt.Sprintf("pending item %q skipped by policy (item %s)", f.label(), f.id))
	case pendingDone:
		st.passPending(next, f, endOutcome{
			edge: edgeOnComplete, memStatus: MemberCompleted, outcome: pendingOutcomeDone, success: true,
		}, fmt.Sprintf("pending item %q is done (item %s)", f.label(), f.id))
	default:
		next.Status = MemberPending
		addCursor(st.a, next.ID)
		st.block(reason)
		st.eff.details["to"] = next.ID
		st.eff.details["pending_item_id"] = f.id
	}
}

// passPending ends a pending item member without an Automaton and follows its
// on_complete edge, as SkipMember does for any member.
func (st *stepper) passPending(mem *AutomationMember, f *pendingFacts, end endOutcome, note string) {
	addCursor(st.a, mem.ID)
	st.finish(mem, end)
	st.eff.details["pending_item_id"] = f.id
	st.decision("pending_item", note)
	st.audit("automation_pending_item", map[string]any{
		"automation_id": st.a.ID, "member": mem.ID, "pending_item_id": f.id,
		"outcome": end.outcome, "actor": coordinatorActor,
	})
	st.followEdge(mem, end)
}

// stepPendingCheck re-evaluates a pending item member that already holds the
// cursor of an active Automation. Anything else (a paused or blocked
// Automation, a member that was converted or has moved on) changes nothing
// and returns errStaleStep, so a replay writes nothing.
func stepPendingCheck(a *Automation, in stepInput, snap map[string]memberInfo) (*stepEffects, error) {
	mem := automationMember(a, in.memberID)
	if a.Status != AutomationActive || mem == nil || mem.kind() != MemberKindPending || !isLiveCursor(a, mem.ID) {
		return nil, errStaleStep
	}
	if mem.Status != MemberPending && mem.Status != "" && mem.Status != MemberRunning {
		return nil, errStaleStep
	}
	st := &stepper{a: a, snap: snap, eff: &stepEffects{details: map[string]any{}}, now: in.at()}
	st.enterPending(mem)
	st.finalize()
	return st.eff, nil
}

// checkPendingCursor re-checks a pending item member that holds the cursor of
// an active Automation. It never starts anything.
func (c *Coordinator) checkPendingCursor(a *Automation, mem *AutomationMember) {
	c.advance(a.ID, stepInput{memberID: mem.ID, pendingCheck: true})
}

// pendingHoldOpen reports whether a is blocked because a pending item member
// holds its cursor, so a resume may lift the block and re-check the item. A
// member converted by an attach keeps its pending_item_id and, until it is
// started, still counts: resuming is what starts it.
func pendingHoldOpen(a *Automation) bool {
	if a == nil || a.Status != AutomationBlocked {
		return false
	}
	for _, id := range liveCursorIDs(a) {
		if mem := automationMember(a, id); mem != nil && mem.PendingItemID != "" && memberUnstarted(mem) {
			return true
		}
	}
	return false
}
