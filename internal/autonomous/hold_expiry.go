// Hold expiry: when an operator hold's look-again time passes, mark it
// expired, record one decision, and raise exactly one alert.
//
// Expiry never changes the status and never starts anything: the Automaton
// stays held, lane fill keeps refusing it (operator_hold), and only an
// operator release or a new hold-until time changes it. The mark is
// persisted, so a second pass, a daemon restart or a store reload raises
// nothing more. A hold with no look-again time never expires.

package autonomous

import (
	"fmt"
	"log"
	"time"
)

// holdExpiredDecisionKind is the audit entry recorded once per expired hold.
const holdExpiredDecisionKind = "hold_expired"

// ExpireHolds is one hold-expiry pass. For every held Automaton whose
// look-again time has passed and that is not yet marked expired, it sets
// ExpiredAt to now, appends one hold_expired Decision, saves, and raises
// exactly one alert through the manager's alert sink. It returns the ids it
// expired. It is idempotent: ExpiredAt is persisted, so a second pass, a
// daemon restart or a reload raises nothing more. It changes no status and
// starts nothing — lane fill keeps skipping the Automaton (operator_hold)
// after expiry — and the alert is not counted as a blocked or review alert
// anywhere.
func (m *Manager) ExpireHolds(now time.Time) []string {
	var expired []string
	for _, prd := range m.store.ListPRDs() {
		if !holdExpiryDue(prd, now) {
			continue
		}
		holdReason := ""
		if prd.Hold != nil {
			holdReason = prd.Hold.Reason
		}
		m.store.WithLock(func() {
			// Re-check under the lock: a concurrent extend may have re-armed
			// the hold (ExpiredAt cleared) or a concurrent pass may have
			// expired it already.
			if !holdExpiryDue(prd, now) {
				return
			}
			at := now
			prd.Hold.ExpiredAt = &at
			prd.UpdatedAt = now
			note := "the hold-until time has passed; the Automaton stays held and is ready to review"
			if u := prd.Hold.Until; u != nil {
				note = "the hold-until time " + u.UTC().Format(time.RFC3339) + " has passed; the Automaton stays held and is ready to review"
			}
			prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: holdExpiredDecisionKind, Actor: "autonomous", Note: note})
		})
		if !holdIsExpired(prd) {
			continue
		}
		if err := m.store.SavePRD(prd); err != nil {
			log.Printf("[autonomous] hold-expiry: save prd=%s: %v", prd.ID, err)
			continue
		}
		expired = append(expired, prd.ID)
		log.Printf("[autonomous] hold-expiry: prd=%s: the hold-until time has passed; the Automaton stays held and is ready to review", prd.ID)
		m.alertHoldExpired(prd, holdReason)
	}
	return expired
}

// holdExpiryDue reports whether the Automaton is held, has a look-again
// time, is not yet marked expired, and now has reached or passed it.
func holdExpiryDue(prd *PRD, now time.Time) bool {
	if prd == nil || prd.Status != PRDHeld || prd.Hold == nil {
		return false
	}
	return prd.Hold.Until != nil && prd.Hold.ExpiredAt == nil && !now.Before(*prd.Hold.Until)
}

// holdIsExpired reports whether the stored record now carries the expiry
// mark.
func holdIsExpired(prd *PRD) bool {
	return prd != nil && prd.Hold != nil && prd.Hold.ExpiredAt != nil
}

// alertHoldExpired raises the one warn alert an expired hold produces. It
// names the Automaton title and the hold reason and says the hold can be
// released or given a new hold-until time. It is not a blocked or review
// alert.
func (m *Manager) alertHoldExpired(prd *PRD, holdReason string) {
	fn := m.AlertSink()
	if fn == nil {
		return
	}
	title := prd.Title
	if title == "" {
		title = prd.ID
	}
	body := fmt.Sprintf("Automaton %q was on hold (reason: %s); its hold-until time has passed. The Automaton stays held and is ready to review: release the hold, or give it a new hold-until time.",
		title, holdReason)
	fn("warn", "Automaton hold expired", body)
}
