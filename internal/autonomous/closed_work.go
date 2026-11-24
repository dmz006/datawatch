// Closed and shipped work never runs. The daemon refuses to start, approve or
// decompose an Automaton whose own record says its work already shipped (a
// release and a merge commit both set), and any Automaton that is archived,
// cancelled, completed or rejected. Queued work with a shipped record is
// dropped from the queue: it ends delivered (completed), never cancelled, with
// the reason on the record. The check reads only the Automaton's own record:
// a shipped record on another Automaton (a dependency, a sibling) never blocks
// it. An operator can lift the rule for one shipped record with an explicit,
// audited force. The rule is the daemon's: it holds with no model cooperating.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Settings of autonomous.shipped_refusal.
const (
	// ShippedRefusalOn enforces the rule (the default).
	ShippedRefusalOn = "on"
	// ShippedRefusalShadow only logs what the rule would have refused.
	ShippedRefusalShadow = "shadow"
	// ShippedRefusalOff turns the rule off.
	ShippedRefusalOff = "off"
)

// Decision kinds written by the closed work rule.
const (
	// DecisionShippedRefusal: queued work with a shipped record was marked
	// delivered instead of started.
	DecisionShippedRefusal = "shipped_refusal"
	// DecisionClosedWorkRefused: a start of closed work was refused.
	DecisionClosedWorkRefused = "closed_work_refused"
	// DecisionClosedWorkOverride: the operator forced past the rule.
	DecisionClosedWorkOverride = "closed_work_override"
	// DecisionMemberDelivered: an Automation skipped a member whose own
	// record says its work already shipped, and went on to the next member.
	DecisionMemberDelivered = "member_delivered"
)

// ShippedRefusalRecord is the reason the daemon dropped an Automaton as
// delivered: what its shipped record said and the status it had before.
type ShippedRefusalRecord struct {
	Reason  string    `json:"reason"`
	Release string    `json:"release"`
	Commit  string    `json:"commit"`
	From    PRDStatus `json:"from"`
	At      time.Time `json:"at"`
}

// ClosedWorkOverride is the operator's audited force past the rule for one
// shipped record. It covers only the merge commit it names: a later shipped
// record with another commit is refused again.
type ClosedWorkOverride struct {
	Commit string    `json:"commit"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// EffectiveShippedRefusal returns the rule's mode; empty or unknown reads as
// on, the default.
func (c Config) EffectiveShippedRefusal() string {
	switch strings.ToLower(strings.TrimSpace(c.ShippedRefusal)) {
	case ShippedRefusalOff:
		return ShippedRefusalOff
	case ShippedRefusalShadow:
		return ShippedRefusalShadow
	}
	return ShippedRefusalOn
}

// hasShippedRecord says the Automaton's own record carries a shipped record:
// both the release and the merge commit are set.
func hasShippedRecord(p *PRD) bool {
	return p != nil && strings.TrimSpace(p.MergedInRelease) != "" && strings.TrimSpace(p.MergeCommit) != ""
}

// overrideCovers says the operator's force names this record's merge commit.
func overrideCovers(p *PRD) bool {
	return p != nil && p.ClosedWorkOverride != nil &&
		p.ClosedWorkOverride.Commit == strings.TrimSpace(p.MergeCommit)
}

// closedWorkStatus says the Automaton has ended for good.
func closedWorkStatus(s PRDStatus) bool {
	switch NormalizePRDStatus(s) {
	case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
		return true
	}
	return false
}

// shippedQueuedStatus says the status is queued work: the ones the sweep drops.
func shippedQueuedStatus(s PRDStatus) bool {
	switch NormalizePRDStatus(s) {
	case PRDDraft, PRDNeedsReview, PRDApproved, PRDRevisionsAsked:
		return true
	}
	return false
}

// shippedDeliverableStatus says a start of the Automaton with a shipped record
// is refused and the Automaton is marked delivered: queued work, or work that
// failed or blocked and was merged by hand. A running, planning, paused or
// held Automaton has its own lifecycle and is left to it.
func shippedDeliverableStatus(s PRDStatus) bool {
	if shippedQueuedStatus(s) {
		return true
	}
	switch NormalizePRDStatus(s) {
	case PRDBlocked, PRDFailed:
		return true
	}
	return false
}

func shortCommit(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 9 {
		return c[:9]
	}
	return c
}

// closedWorkRefusal returns the code and the readable text of the reason the
// Automaton must not start, or two empty strings. Only the Automaton's own
// record is read. The caller holds the store lock or passes a snapshot.
//
//   - work_closed: the status is completed, cancelled, archived or rejected;
//   - already_shipped: its own shipped record is set, the status is queued,
//     failed or blocked, and no override covers the record.
func closedWorkRefusal(p *PRD) (code, text string) {
	if p == nil {
		return "", ""
	}
	if closedWorkStatus(p.Status) {
		text = fmt.Sprintf("is %s (closed work): it never runs", NormalizePRDStatus(p.Status))
		if hasShippedRecord(p) {
			text += fmt.Sprintf("; its work already shipped in %s (commit %s)", strings.TrimSpace(p.MergedInRelease), shortCommit(p.MergeCommit))
		}
		return LaneFillReasonWorkClosed, text
	}
	if hasShippedRecord(p) && shippedDeliverableStatus(p.Status) && !overrideCovers(p) {
		return LaneFillReasonAlreadyShipped, fmt.Sprintf("already shipped in %s (commit %s)",
			strings.TrimSpace(p.MergedInRelease), shortCommit(p.MergeCommit))
	}
	return "", ""
}

// shadowNoted keeps the shadow log to one line per Automaton and reason.
var shadowNoted sync.Map

// refuseClosedWork is the guard every door toward a start calls after
// refuseHeld. It returns an error naming the reason when the Automaton is
// closed or already shipped, and nil otherwise. Shipped queued work is marked
// delivered as a side effect, so it drops out of every queue; closed work is
// left as it is and the refusal is recorded once. With the rule in shadow mode
// it only logs, and with the rule off it does nothing. Call it with no store
// lock held.
func (m *Manager) refuseClosedWork(prd *PRD) error {
	if m == nil || prd == nil {
		return nil
	}
	mode := m.Config().EffectiveShippedRefusal()
	if mode == ShippedRefusalOff {
		return nil
	}
	var code, text string
	m.readLocked(func() { code, text = closedWorkRefusal(prd) })
	if code == "" {
		return nil
	}
	if mode == ShippedRefusalShadow {
		if _, seen := shadowNoted.LoadOrStore(prd.ID+"/"+code, true); !seen {
			log.Printf("[autonomous] shipped-refusal shadow: automaton %s %s (would be refused)", prd.ID, text)
		}
		return nil
	}
	if code == LaneFillReasonAlreadyShipped {
		m.deliverShipped(prd.ID)
		return fmt.Errorf("automaton %q %s; it is marked delivered and does not run (to run it anyway, force with a reason)", prd.ID, text)
	}
	m.recordClosedRefusal(prd.ID, text)
	return fmt.Errorf("automaton %q %s", prd.ID, text)
}

// recordClosedRefusal writes the closed_work_refused decision once per
// distinct reason, so a client retrying a start does not grow the record.
func (m *Manager) recordClosedRefusal(id, text string) {
	note := fmt.Sprintf("Automaton %s %s; a start was refused.", id, text)
	at := m.holdNow()
	m.store.MutatePRD(id, func(p *PRD) bool {
		if d := lastDecision(p, DecisionClosedWorkRefused); d != nil && d.Note == note {
			return false
		}
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedWorkRefused, Actor: "autonomous", Note: note}))
		return true
	})
}

// deliverShipped marks one Automaton delivered because its own record says its
// work shipped: status completed (never cancelled), the reason on the record
// as a decision and as ShippedRefusal. It reports whether anything changed;
// a record that no longer qualifies, or that moved meanwhile, is left alone.
func (m *Manager) deliverShipped(id string) bool {
	rev, ok := m.store.PRDRev(id)
	if !ok {
		return false
	}
	at := m.holdNow()
	done := false
	err := m.store.CommitPRDIfRev(id, rev, func(p *PRD) bool {
		code, text := closedWorkRefusal(p)
		if code != LaneFillReasonAlreadyShipped {
			return false
		}
		from := p.Status
		p.ShippedRefusal = &ShippedRefusalRecord{
			Reason: text, Release: strings.TrimSpace(p.MergedInRelease),
			Commit: strings.TrimSpace(p.MergeCommit), From: from, At: at,
		}
		p.Status = PRDCompleted
		p.BlockedReason = ""
		p.Decisions = append(p.Decisions, Decision{
			At: at, Kind: DecisionShippedRefusal, Actor: "autonomous",
			Note: fmt.Sprintf("Automaton %s %s, so it did not start: marked delivered (completed), not cancelled, and dropped from the queue (it was %s). To run it anyway, force with a reason.", p.ID, text, from),
		})
		done = true
		return true
	})
	if err != nil || !done {
		return false
	}
	log.Printf("[autonomous] automaton=%s dropped as delivered: shipped record already set", id)
	m.EmitPRDUpdate(id)
	return true
}

// DeliverShippedQueued drops every queued Automaton (draft, needs_review,
// approved, revisions_asked) whose own record says its work shipped, marking
// each delivered. A held, running, planning or paused Automaton is never
// touched. It returns the ids it changed. The coordinator calls it on every
// poll, so a queue never keeps shipped work.
func (m *Manager) DeliverShippedQueued() []string {
	if m == nil {
		return nil
	}
	mode := m.Config().EffectiveShippedRefusal()
	if mode == ShippedRefusalOff {
		return nil
	}
	var ids []string
	prds := m.store.ListPRDs()
	m.readLocked(func() {
		for _, p := range prds {
			if p == nil || p.IsTemplate || !shippedQueuedStatus(p.Status) {
				continue
			}
			if code, _ := closedWorkRefusal(p); code == LaneFillReasonAlreadyShipped {
				ids = append(ids, p.ID)
			}
		}
	})
	var out []string
	for _, id := range ids {
		if mode == ShippedRefusalShadow {
			p, ok := m.store.GetPRD(id)
			if ok {
				_ = m.refuseClosedWork(p) // logs once, changes nothing
			}
			continue
		}
		if m.deliverShipped(id) {
			out = append(out, id)
		}
	}
	return out
}

// ForceClosedWork is the operator's explicit, audited override for one shipped
// record: it names the merge commit it covers, records who and why on the
// Automaton, and, when the daemon itself dropped the Automaton as delivered,
// restores the status it had so it can run again. Closed work (archived,
// cancelled, rejected, or completed by any other route) cannot be forced.
func (m *Manager) ForceClosedWork(id, actor, reason string) (*PRD, error) {
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if actor == "" {
		actor = "operator"
	}
	if reason == "" {
		return nil, fmt.Errorf("force needs a reason: say why shipped work should run again")
	}
	rev, ok := m.store.PRDRev(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	at := m.holdNow()
	var refuse error
	err := m.store.CommitPRDIfRev(id, rev, func(p *PRD) bool {
		if !hasShippedRecord(p) {
			refuse = fmt.Errorf("automaton %q has no shipped record: there is nothing to force", id)
			return false
		}
		commit := strings.TrimSpace(p.MergeCommit)
		if overrideCovers(p) {
			refuse = fmt.Errorf("automaton %q already carries a force for commit %s", id, shortCommit(commit))
			return false
		}
		restored := PRDStatus("")
		if closedWorkStatus(p.Status) {
			rec := p.ShippedRefusal
			if NormalizePRDStatus(p.Status) != PRDCompleted || rec == nil || rec.Commit != commit {
				refuse = fmt.Errorf("automaton %q is %s (closed work): it cannot be forced to run", id, NormalizePRDStatus(p.Status))
				return false
			}
			restored = rec.From
			p.Status = rec.From
			p.ShippedRefusal = nil
		}
		p.ClosedWorkOverride = &ClosedWorkOverride{Commit: commit, Actor: actor, Reason: reason, At: at}
		note := fmt.Sprintf("Forced past the shipped-work rule for commit %s (release %s): %s.", shortCommit(commit), strings.TrimSpace(p.MergedInRelease), reason)
		if restored != "" {
			note += fmt.Sprintf(" The daemon had marked it delivered; its status is back to %s.", restored)
		}
		p.Decisions = append(p.Decisions, Decision{At: at, Kind: DecisionClosedWorkOverride, Actor: actor, Note: note})
		return true
	})
	if refuse != nil {
		return nil, refuse
	}
	if err != nil {
		return nil, fmt.Errorf("force: %w", err)
	}
	m.EmitPRDUpdate(id)
	out, _ := m.store.GetPRD(id)
	return out, nil
}
