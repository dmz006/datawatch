// Automation boot reconcile: after a daemon restart the lifecycle events that
// fired while the daemon was down are gone, so the coordinator re-derives each
// active or paused Automation's position from the member PRD's stored status
// (design doc "Trigger mechanism"). It runs once, at the end of
// API.resumeRunningPRDs, after the PRDFailed recovery pass, so a member the
// daemon killed is not routed down its on_failed edge.

package autonomous

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// reconcile walks every active or paused Automation once and brings its
// cursor in line with the member PRD. It marks the coordinator ready first, so
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

func (c *Coordinator) reconcileOne(a *Automation) {
	mem := automationMember(a, a.CurrentMemberID)
	if mem == nil {
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
	release := c.beginStart(prdID, reason == "")
	defer release()
	updated, err := c.m.automations.Update(a.ID, a.Version, func(a *Automation) error {
		cur := automationMember(a, memID)
		if a.Status != AutomationActive || a.CurrentMemberID != memID || cur == nil || (cur.Status != MemberPending && cur.Status != "") {
			return errors.New("409: member already handled")
		}
		now := time.Now()
		if reason != "" {
			a.Status = AutomationBlocked
			a.IdleSince = &now
			a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "blocked", Actor: coordinatorActor, Note: reason}))
			return nil
		}
		cur.Status = MemberRunning
		cur.StartedAt = &now
		a.IdleSince = nil
		a.Decisions = trimDecisions(append(a.Decisions, Decision{At: now, Kind: "advance", Actor: coordinatorActor, Note: fmt.Sprintf("restart: retried start of %s", name)}))
		return nil
	})
	if err != nil {
		if !strings.HasPrefix(err.Error(), "409") {
			log.Printf("[autonomous] automation %s reconcile start of member %s: %v", a.ID, memID, err)
		}
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
