// Reopened-member handling in the Automation coordinator. A member PRD that was
// cancelled (so its Automation went blocked on it) and is then run again must be
// followed: the member goes back to running and the Automation back to active,
// as if the operator had resumed it. A reopened member never revives an
// Automation that is cancelled, completed, paused or otherwise not blocked on
// that member.
package autonomous

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// decisionReopened is the Automation Decision kind recorded when a cancelled
// member was run again and the chain resumed.
const decisionReopened = "reopened"

// reopenable reports whether a is blocked on member mem because the member PRD
// was cancelled outside the Automation. A linear Automation must still hold its
// cursor on mem; the other Automation statuses (active, paused, cancelled,
// completed, ...) are never reopened.
func reopenable(a *Automation, mem *AutomationMember) bool {
	if a.Status != AutomationBlocked || mem == nil || mem.Status != MemberBlocked {
		return false
	}
	switch mem.Outcome {
	case string(PRDCancelled), string(PRDArchived), string(PRDRejected):
	default:
		return false
	}
	if automationIsLinear(a) {
		return a.CurrentMemberID == mem.ID
	}
	return true
}

// onMemberReopened handles a member PRD moving back to running. When the
// Automation is blocked on that member because of a cancellation, it sets the
// member running, the Automation active and records a Decision. It starts
// nothing: the member is already running. The start mark is claimed around the
// write, so a start the coordinator itself has in flight (whose running event
// this is) or an overlapping reopen is not treated as a second run.
func (c *Coordinator) onMemberReopened(autoID, prdID string) {
	a, ok := c.m.automations.Get(autoID)
	if !ok || !reopenable(a, memberByPRD(a, prdID)) {
		return
	}
	release, ok := c.claimStart(prdID)
	if !ok {
		return
	}
	defer release()

	name, _, _ := c.prdState(prdID)
	for attempt := 0; attempt < coordinatorCASRetries; attempt++ {
		a, ok := c.m.automations.Get(autoID)
		if !ok || !reopenable(a, memberByPRD(a, prdID)) {
			return
		}
		var memID string
		updated, err := c.m.automations.Update(a.ID, a.Version, func(cur *Automation) error {
			mem := memberByPRD(cur, prdID)
			if !reopenable(cur, mem) {
				return errStaleStep
			}
			memID = mem.ID
			mem.Status = MemberRunning
			mem.FinishedAt = nil
			mem.Outcome = ""
			mem.EdgeTaken = ""
			if !isLiveCursor(cur, mem.ID) {
				addCursor(cur, mem.ID)
			}
			if !automationIsLinear(cur) {
				cur.CurrentMemberID = firstCursor(cur.ActiveCursors)
			}
			cur.Status = AutomationActive
			cur.IdleSince = nil
			cur.Decisions = trimDecisions(append(cur.Decisions, Decision{
				At: c.now(), Kind: decisionReopened, Actor: coordinatorActor,
				Note: fmt.Sprintf("Automaton reopened: member %s was run again after it was cancelled; the chain resumed", name),
			}))
			return nil
		})
		if err != nil {
			if strings.HasPrefix(err.Error(), "409") {
				continue // version moved: re-read and re-evaluate
			}
			if !errors.Is(err, errStaleStep) {
				log.Printf("[autonomous] automation %s reopen on member %s: %v", autoID, prdID, err)
			}
			return
		}
		c.audit("automation_reopened", map[string]any{
			"automation_id": updated.ID, "member": memID, "prd_id": prdID,
			"actor": coordinatorActor, "at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	log.Printf("[autonomous] automation %s: reopen on member %s dropped after %d version conflicts", autoID, prdID, coordinatorCASRetries)
}
