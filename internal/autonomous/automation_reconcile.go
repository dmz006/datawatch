// Automation boot reconcile: after a daemon restart the lifecycle events that
// fired while the daemon was down are gone, so the coordinator re-derives each
// active or paused Automation's position from the member PRD's stored status
// (design doc "Trigger mechanism"). It runs once, at the end of
// API.resumeRunningPRDs, after the PRDFailed recovery pass, so a member the
// daemon killed is not routed down its on_failed edge.
//
// For a multi-cursor Automation the pass covers every live cursor, every
// waiting join and every nested child (design doc "Restart reconciliation
// (multi-cursor)"). Each step re-derives its answer from stored member statuses,
// so a second pass changes nothing.

package autonomous

import (
	"errors"
	"fmt"
	"log"
	"strings"
)

// reconcile walks every active or paused Automation once and brings its
// cursors in line with the member PRDs. It marks the coordinator ready first, so
// failures deferred during boot are replayed, and is idempotent: a second run
// finds each cursor member either running or already handled. It is nil-safe.
func (c *Coordinator) reconcile() {
	if c == nil {
		return
	}
	c.markReady()
	for _, a := range c.m.automations.List() {
		if a.Status != AutomationActive && a.Status != AutomationPaused {
			continue
		}
		c.reconcileOne(a)
	}
}

// reconcileOne reconciles one Automation: each live cursor, then each join still
// waiting. It re-reads the Automation before every cursor, so an advance made
// earlier in the pass is seen, and a cursor that has already moved on is skipped.
func (c *Coordinator) reconcileOne(a *Automation) {
	for _, id := range liveCursorIDs(a) {
		cur, ok := c.m.automations.Get(a.ID)
		if !ok || (cur.Status != AutomationActive && cur.Status != AutomationPaused) || !isLiveCursor(cur, id) {
			continue
		}
		if mem := automationMember(cur, id); mem != nil {
			c.reconcileCursor(cur, mem)
		}
	}
	c.reconcileJoins(a.ID)
}

// reconcileCursor brings one live cursor in line with its member.
func (c *Coordinator) reconcileCursor(a *Automation, mem *AutomationMember) {
	if k := mem.kind(); (k == MemberKindAutomation || k == MemberKindFork) && memberUnstarted(mem) {
		// A deferred member (the budget gate, or a pause, held it back).
		c.startPending(a, mem)
		return
	}
	if mem.kind() == MemberKindAutomation {
		c.reconcileNested(a, mem)
		return
	}
	// Checked before the PRD status is read: a start in flight has marked the
	// member running but may not have moved the PRD yet.
	if c.isStarting(mem.PRDID) {
		return
	}
	_, status, ok := c.prdState(mem.PRDID)
	if !ok {
		return
	}
	if mem.Status == MemberRunning && status == PRDApproved && joinOf(a, mem.ID) != nil {
		c.retryBranchStart(a, mem)
		return
	}
	switch mem.Status {
	case MemberRunning:
		switch status {
		case PRDCompleted, PRDBlocked, PRDFailed, PRDCancelled, PRDArchived, PRDRejected:
			// Terminal while the member is still marked running: the event was
			// missed. onMemberTerminal compare-and-sets, so a live event for
			// the same PRD cannot advance the cursor twice.
			c.reconcileAudit(a, mem, "route terminal member", string(status))
			c.onMemberTerminal(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(status), PRDID: mem.PRDID, NewStatus: string(status)})
		case PRDApproved:
			// Started, but the PRD is back at approved and not running: the
			// run errored before or during the restart.
			if mem.StartedAt == nil {
				return
			}
			c.reconcileAudit(a, mem, "member run errored", string(status))
			c.onMemberTerminal(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(status), PRDID: mem.PRDID, NewStatus: string(status)})
		}
		// PRDRunning: boot-resume owns it. Anything else waits.
	case MemberPending, "": // an unset status is a member never started
		if a.Status == AutomationActive {
			c.retryPendingStart(a, mem)
		}
	}
}

// retryBranchStart gives a fork branch exactly one start attempt when the daemon
// died after the fork's write and before the branch's start call: the branch is
// marked running but its PRD is still approved. A branch whose PRD already runs
// started before the restart and is left to boot-resume. The claim makes the
// attempt happen once even if two passes overlap, and the attempt's outcome is
// final: a refused start ends the branch as a failed branch, a successful one
// leaves the PRD running, so the next pass sees a started branch.
func (c *Coordinator) retryBranchStart(a *Automation, mem *AutomationMember) {
	if a.Status != AutomationActive {
		return // a paused Automation starts nothing
	}
	release, ok := c.claimStart(mem.PRDID)
	if !ok {
		return
	}
	defer release()
	name, status, ok := c.prdState(mem.PRDID)
	if !ok || status != PRDApproved {
		return
	}
	c.reconcileAudit(a, mem, "retry branch start", string(status))
	c.launch(a.ID, memberStart{memberID: mem.ID, kind: MemberKindPRD, prdID: mem.PRDID, name: name, branch: true})
}

// reconcileNested gives a nested member exactly one activation attempt when its
// child Automation is still a draft: the daemon died after the member was marked
// running and before the child was activated. A child that has left draft, or an
// attempt another pass holds, is left alone.
func (c *Coordinator) reconcileNested(a *Automation, mem *AutomationMember) {
	if a.Status != AutomationActive || mem.Status != MemberRunning {
		return // a paused Automation starts nothing
	}
	child, ok := c.m.automations.Get(mem.AutomationID)
	if !ok || child.Status != AutomationDraft {
		return
	}
	release, ok := c.claimStart("automation/" + child.ID)
	if !ok {
		return
	}
	defer release()
	name := child.Name
	if name == "" {
		name = child.ID
	}
	c.reconcileAudit(a, mem, "retry nested activation", string(child.Status))
	c.launch(a.ID, memberStart{
		memberID: mem.ID, kind: MemberKindAutomation, childID: child.ID,
		name: name, branch: joinOf(a, mem.ID) != nil,
	})
}

// reconcileJoins re-derives every join still waiting. A join whose branches are
// all terminal fires or blocks here, once: stepJoinReeval changes nothing after
// the first apply, and a join with a live branch is left waiting.
func (c *Coordinator) reconcileJoins(autoID string) {
	a, ok := c.m.automations.Get(autoID)
	if !ok {
		return
	}
	for _, m := range a.Members {
		if m.kind() == MemberKindJoin && m.Status == MemberRunning {
			c.advance(autoID, stepInput{memberID: m.ID, reeval: true})
		}
	}
}

// retryPendingStart starts a cursor member that never got started, through
// the same approval gate the coordinator uses between members. The member is
// marked running by a compare-and-set before the start, so concurrent
// reconciles start it once.
func (c *Coordinator) retryPendingStart(a *Automation, mem *AutomationMember) {
	memID, prdID := mem.ID, mem.PRDID
	name, _, _ := c.prdState(prdID)
	mode, status := c.startGate(prdID)
	reason := ""
	if mode == "" {
		reason = fmt.Sprintf("next member %s is %s", name, status)
	}
	// The budget gate guards a retried start as it does every other start.
	var verdict BudgetVerdict
	if reason == "" {
		verdict = c.m.budgetVerdictFor(a, mem)
		if verdict.Gate && budgetNoChange(a, verdict) {
			return // still held for the episode already recorded
		}
	}
	release := c.beginStart(prdID, reason == "" && !verdict.Gate)
	defer release()
	var beff *budgetEffect
	updated, err := c.m.automations.Update(a.ID, a.Version, func(a *Automation) error {
		cur := automationMember(a, memID)
		if a.Status != AutomationActive || !isLiveCursor(a, memID) || cur == nil || (cur.Status != MemberPending && cur.Status != "") {
			return errors.New("409: member already handled")
		}
		now := c.now()
		beff = nil
		if reason == "" && verdict.Gate {
			beff = applyBudgetGate(a, cur, verdict, now, false)
			if beff.action == BudgetActionBlocked {
				a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "blocked", Actor: coordinatorActor, Note: beff.reason}))
			}
			return nil
		}
		if reason != "" {
			a.Status = AutomationBlocked
			a.IdleSince = &now
			a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "blocked", Actor: coordinatorActor, Note: reason}))
			return nil
		}
		cur.Status = MemberRunning
		cur.StartedAt = &now
		a.IdleSince = nil
		a.BudgetState = nil
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "advance", Actor: coordinatorActor, Note: fmt.Sprintf("restart: retried start of %s", name)}))
		return nil
	})
	if err != nil {
		if !strings.HasPrefix(err.Error(), "409") {
			log.Printf("[autonomous] automation %s reconcile start of member %s: %v", a.ID, memID, err)
		}
		return
	}
	if beff != nil {
		c.budgetBlockedAlert(updated, beff, memID)
		return
	}
	if reason != "" {
		c.reconcileAudit(updated, mem, reason, string(status))
		c.blocked(updated, reason, map[string]any{"to": memID})
		return
	}
	c.reconcileAudit(updated, mem, "retry pending start", string(status))
	c.startMember(updated.ID, memID, prdID, name, mode == "resume")
}

func (c *Coordinator) reconcileAudit(a *Automation, mem *AutomationMember, reason, outcome string) {
	c.audit("automation_reconcile", map[string]any{
		"automation_id": a.ID, "member": mem.ID, "prd_id": mem.PRDID,
		"outcome": outcome, "reason": reason, "actor": coordinatorActor,
	})
}
