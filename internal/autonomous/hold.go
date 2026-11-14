// Operator hold on an Automaton: put one on hold, change the hold, release it.
//
// A held Automaton waits by operator decision. It is not blocked, not failed
// and not waiting for review, and nothing starts it. Holding never touches a
// task or a story. Expiry of the look-again time never changes the status;
// only a release (back to the status it had) or an extend changes it.

package autonomous

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// holdReasonMaxChars is the longest hold reason, counted in characters after
// trimming.
const holdReasonMaxChars = 500

// holdAllowedFrom lists the statuses an Automaton may be put on hold from,
// in the order an error message names them.
var holdAllowedFrom = []PRDStatus{
	PRDDraft, PRDNeedsReview, PRDApproved, PRDRevisionsAsked,
	PRDPaused, PRDBlocked, PRDFailed,
}

// HoldRequest is what an operator supplies to hold or extend a hold. Reason
// is required to hold; Condition is free text shown to the operator and never
// evaluated; Until is the optional look-again time.
type HoldRequest struct {
	Reason    string
	Condition string
	Until     *time.Time
}

// holdNow reads the manager's clock; tests set Manager.now to a fake.
func (m *Manager) holdNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// holdAllowedFromList names the statuses a hold may start from, for errors.
func holdAllowedFromList() string {
	names := make([]string, len(holdAllowedFrom))
	for i, s := range holdAllowedFrom {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// holdAllowsStatus reports whether a hold may start from s.
func holdAllowsStatus(s PRDStatus) bool {
	for _, a := range holdAllowedFrom {
		if a == s {
			return true
		}
	}
	return false
}

// checkHoldReason trims reason and checks it is present and short enough.
func checkHoldReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("a hold needs a reason; say why the Automaton is waiting")
	}
	if n := utf8.RuneCountInString(reason); n > holdReasonMaxChars {
		return "", fmt.Errorf("the hold reason is %d characters; the most allowed is %d", n, holdReasonMaxChars)
	}
	return reason, nil
}

// checkHoldUntil checks that a look-again time, when given, is after now.
func checkHoldUntil(until *time.Time, now time.Time) error {
	if until != nil && !until.After(now) {
		return fmt.Errorf("the hold-until time %s is not in the future; give a later time or leave it out",
			until.UTC().Format(time.RFC3339))
	}
	return nil
}

// holdDecisionNote is the audit note for a hold or extend entry.
func holdDecisionNote(h *HoldState) string {
	note := "reason=" + h.Reason
	if h.Condition != "" {
		note += "; condition=" + h.Condition
	}
	if h.Until != nil {
		note += "; until=" + h.Until.UTC().Format(time.RFC3339)
	}
	return note
}

// Hold puts an Automaton on hold. It is allowed from draft, needs_review,
// approved, revisions_asked, paused, blocked and failed. It records the status
// the Automaton had (From), the time, the actor and, for a blocked Automaton,
// the blocked reason (the store clears BlockedReason whenever the status is not
// blocked, so the hold keeps its own copy). It never touches a task or a story
// and does not start an executor.
func (m *Manager) Hold(prdID string, req HoldRequest, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", prdID)
	}
	reason, err := checkHoldReason(req.Reason)
	if err != nil {
		return nil, err
	}
	now := m.holdNow()
	if err := checkHoldUntil(req.Until, now); err != nil {
		return nil, err
	}
	var old PRDStatus
	var refused error
	m.store.WithLock(func() {
		old = prd.Status
		if old == PRDHeld {
			refused = fmt.Errorf("automaton %q is already held; extend the hold to change it or release it", prdID)
			return
		}
		if !holdAllowsStatus(old) {
			refused = fmt.Errorf("automaton %q status %q cannot be put on hold; a hold is allowed from %s",
				prdID, old, holdAllowedFromList())
			return
		}
		h := &HoldState{
			Reason:    reason,
			Condition: strings.TrimSpace(req.Condition),
			HeldAt:    now,
			HeldBy:    actor,
			From:      old,
		}
		if req.Until != nil {
			u := *req.Until
			h.Until = &u
		}
		if old == PRDBlocked {
			h.PriorBlockedReason = prd.BlockedReason
		}
		prd.Hold = h
		prd.Status = PRDHeld
		prd.BlockedReason = ""
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "hold", Actor: actor, Note: holdDecisionNote(h)})
	})
	if refused != nil {
		return nil, refused
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "hold"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ExtendHold changes the hold on a held Automaton. Each field the request
// supplies replaces the stored one (a reason, a condition, a new look-again
// time); a field left empty is kept. The expiry mark is cleared, so an
// Automaton shown as hold expired is waiting again. The status stays held.
func (m *Manager) ExtendHold(prdID string, req HoldRequest, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", prdID)
	}
	reason := ""
	if strings.TrimSpace(req.Reason) != "" {
		var err error
		if reason, err = checkHoldReason(req.Reason); err != nil {
			return nil, err
		}
	}
	now := m.holdNow()
	if err := checkHoldUntil(req.Until, now); err != nil {
		return nil, err
	}
	var refused error
	m.store.WithLock(func() {
		if prd.Status != PRDHeld || prd.Hold == nil {
			refused = fmt.Errorf("automaton %q status %q is not held; only a held Automaton's hold can be extended", prdID, prd.Status)
			return
		}
		h := prd.Hold
		if reason != "" {
			h.Reason = reason
		}
		if c := strings.TrimSpace(req.Condition); c != "" {
			h.Condition = c
		}
		if req.Until != nil {
			u := *req.Until
			h.Until = &u
		}
		h.ExpiredAt = nil
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "hold_extend", Actor: actor, Note: holdDecisionNote(h)})
	})
	if refused != nil {
		return nil, refused
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, PRDHeld, lastDecision(prd, "hold_extend"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// ReleaseHold ends the hold on a held Automaton and restores the status it had
// when it was held. When that status was blocked, the blocked reason comes back
// too. The hold record is cleared and the note goes into the audit entry. It
// does not start an executor.
func (m *Manager) ReleaseHold(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", prdID)
	}
	now := m.holdNow()
	var refused error
	m.store.WithLock(func() {
		if prd.Status != PRDHeld || prd.Hold == nil {
			refused = fmt.Errorf("automaton %q status %q is not held; only a held Automaton can be released", prdID, prd.Status)
			return
		}
		h := prd.Hold
		prd.Status = h.From
		if h.From == PRDBlocked {
			prd.BlockedReason = h.PriorBlockedReason
		}
		prd.Hold = nil
		prd.UpdatedAt = now
		detail := "restored=" + string(h.From)
		if n := strings.TrimSpace(note); n != "" {
			detail += "; note=" + n
		}
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "hold_release", Actor: actor, Note: detail})
	})
	if refused != nil {
		return nil, refused
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, PRDHeld, lastDecision(prd, "hold_release"), nil)
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}
