// Closed work outcome: the class a finished Automaton ended in. "Archived"
// mixes delivered, superseded and dropped work, so the record carries its own
// class: delivered (the work is on main, by this record or by a twin named in
// delivered_via), superseded (replaced by another Automaton), dropped (thrown
// away on purpose) or held (waiting by decision, never closed). The daemon
// owns the rule, so it holds with no model cooperating:
//
//   - Cancel, reject and archive record the class in the same step. A shipped
//     record means delivered, a rejection means dropped, a cancel with work
//     left undone means dropped; anything else is not guessed and the closing
//     step asks for an explicit outcome.
//   - The archive step refuses a record with no outcome or with a hold that
//     is not met (outcome held, a hold record not yet released or expired, or
//     a spec that declares its own hold and has no recorded outcome).
//   - A sweep never changes the status of running, planning or held work, and
//     never drops a hold.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// Closed outcome classes.
const (
	OutcomeDelivered  = "delivered"
	OutcomeSuperseded = "superseded"
	OutcomeDropped    = "dropped"
	OutcomeHeld       = "held"
)

// Modes of autonomous.closed_outcome_archive and autonomous.closed_outcome_check.
const (
	ClosedOutcomeOn     = "on"
	ClosedOutcomeShadow = "shadow"
	ClosedOutcomeOff    = "off"
)

// Decision kinds written by the closed outcome rule.
const (
	// DecisionClosedOutcome: the outcome class was recorded or changed.
	DecisionClosedOutcome = "closed_outcome"
	// DecisionClosedOutcomeRefused: an archive was refused for want of an
	// outcome or because of an unmet hold.
	DecisionClosedOutcomeRefused = "closed_outcome_refused"
)

// ErrClosedOutcomeRefused wraps the error of an archive refused for want of an
// outcome or because of an unmet hold; a sweep counts it as kept, not failed.
var ErrClosedOutcomeRefused = errors.New("closed outcome refused")

// ValidClosedOutcome reports whether s is one of the four outcome classes.
func ValidClosedOutcome(s string) bool {
	switch s {
	case OutcomeDelivered, OutcomeSuperseded, OutcomeDropped, OutcomeHeld:
		return true
	}
	return false
}

// OutcomeInput is what a closing step may say about the outcome. A zero value
// asks the daemon to derive it from the record.
type OutcomeInput struct {
	// Outcome is delivered, superseded, dropped or held; empty derives it.
	Outcome string
	// DeliveredVia is the id of the twin that delivered the work, or of the
	// Automaton that replaced it (superseded).
	DeliveredVia string
	// Note says why, in a sentence.
	Note string
}

// DeployRecord is the post-ship deploy of a release: what was deployed, when,
// on which host, and how the health check went. It is written by the
// deploy-after-ship step (or by the operator's own deploy script through the
// record surfaces) onto every Automaton that shipped in that release.
type DeployRecord struct {
	// Release is the shipped release the deploy carried; Version is the
	// version the host runs after it (equal to Release when the host reports
	// nothing else).
	Release string    `json:"release,omitempty"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	Host    string    `json:"host"`
	// Health is passed, failed or unchecked (the deploy step ran no check).
	Health string `json:"health"`
	Detail string `json:"detail,omitempty"`
}

// Deploy health results.
const (
	DeployHealthPassed    = "passed"
	DeployHealthFailed    = "failed"
	DeployHealthUnchecked = "unchecked"
)

// ValidDeployHealth reports whether s is one of the three health results.
func ValidDeployHealth(s string) bool {
	switch s {
	case DeployHealthPassed, DeployHealthFailed, DeployHealthUnchecked:
		return true
	}
	return false
}

// resolveClosedOutcomeMode maps a stored mode to on, shadow or off; empty or
// unknown reads as def.
func resolveClosedOutcomeMode(v, def string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ClosedOutcomeOn:
		return ClosedOutcomeOn
	case ClosedOutcomeShadow:
		return ClosedOutcomeShadow
	case ClosedOutcomeOff:
		return ClosedOutcomeOff
	}
	return def
}

// EffectiveClosedOutcomeArchive returns the archive guard's mode; empty reads
// as on, the default (the same reading as the daemon config). A manager built
// from DefaultConfig starts in shadow, like the other manager-level fixtures.
func (c Config) EffectiveClosedOutcomeArchive() string {
	return resolveClosedOutcomeMode(c.ClosedOutcomeArchive, ClosedOutcomeOn)
}

// EffectiveClosedOutcomeCheck returns the standing check's mode; empty reads
// as shadow, the default.
func (c Config) EffectiveClosedOutcomeCheck() string {
	return resolveClosedOutcomeMode(c.ClosedOutcomeCheck, ClosedOutcomeShadow)
}

// specHoldRe finds the words a spec uses to declare its own hold. Only the
// unambiguous forms count: the capitalised word HOLD, "STATUS: HOLD", and the
// plain phrases below.
var specHoldRe = regexp.MustCompile(`(\bHOLD\b|(?i:do not (approve|run)\b|held until\b|until the operator says\b|needs explicit operator go-ahead\b))`)

// specDeclaresHold reports whether the Automaton's own spec declares a hold.
func specDeclaresHold(p *PRD) bool {
	return p != nil && specHoldRe.MatchString(p.Spec)
}

// holdUnmet reports whether the record still carries a hold that is not met:
// a hold record not released and not expired by its own time.
func holdUnmet(p *PRD, now time.Time) bool {
	if p == nil || p.Hold == nil {
		return false
	}
	if p.Hold.ExpiredAt != nil {
		return false
	}
	if p.Hold.Until != nil && !p.Hold.Until.After(now) {
		return false
	}
	return true
}

// deriveClosedOutcome is the class the record's own facts prove, with no
// guessing: a shipped record means delivered; a rejection, or a cancel with
// work left undone, means dropped. Anything else returns empty and the closing
// step asks for an explicit outcome.
func deriveClosedOutcome(p *PRD) (outcome, note string) {
	if p == nil {
		return "", ""
	}
	if hasShippedRecord(p) {
		return OutcomeDelivered, fmt.Sprintf("shipped in %s (commit %s)", strings.TrimSpace(p.MergedInRelease), shortCommit(p.MergeCommit))
	}
	switch NormalizePRDStatus(p.Status) {
	case PRDRejected:
		return OutcomeDropped, "rejected before it ran"
	case PRDCancelled:
		if !allTasksCompleted(p) {
			return OutcomeDropped, "cancelled before its work finished"
		}
	}
	return "", ""
}

// applyClosedOutcome writes the outcome fields. The caller holds the store lock.
func applyClosedOutcome(p *PRD, outcome, via, note string, at time.Time) {
	p.ClosedOutcome, p.DeliveredVia, p.ClosedOutcomeNote = outcome, via, note
	t := at
	p.ClosedOutcomeAt = &t
}

// validateOutcomeInput checks an explicit outcome against the store: a known
// class; for delivered and superseded a twin that exists, is not the record
// itself, and (delivered) has a shipped record of its own. An empty class is
// valid (derive).
func (m *Manager) validateOutcomeInput(id string, in OutcomeInput) error {
	if in.Outcome == "" {
		if in.DeliveredVia != "" {
			return fmt.Errorf("delivered_via needs an outcome (delivered or superseded)")
		}
		return nil
	}
	if !ValidClosedOutcome(in.Outcome) {
		return fmt.Errorf("outcome %q is not one of delivered, superseded, dropped, held", in.Outcome)
	}
	via := strings.TrimSpace(in.DeliveredVia)
	switch in.Outcome {
	case OutcomeSuperseded:
		if via == "" {
			return fmt.Errorf("superseded work must name the Automaton that replaced it (delivered_via)")
		}
	case OutcomeDelivered:
		// delivered by this record's own shipped record needs no twin
	default:
		if via != "" {
			return fmt.Errorf("delivered_via applies to delivered and superseded work, not %s", in.Outcome)
		}
	}
	if via == "" {
		return nil
	}
	if via == id {
		return fmt.Errorf("an Automaton cannot be its own twin (delivered_via %s)", via)
	}
	twin, ok := m.store.GetPRD(via)
	if !ok {
		return fmt.Errorf("twin %q not found", via)
	}
	if in.Outcome == OutcomeDelivered {
		var shipped bool
		m.readLocked(func() { shipped = hasShippedRecord(twin) })
		if !shipped {
			return fmt.Errorf("twin %q has no shipped record (release and merge commit), so it cannot have delivered the work", via)
		}
	}
	return nil
}

// closeRefusal returns the plain-language reason the archive step must refuse
// the record, or "". in is the explicit outcome the caller supplied. The
// caller holds the store lock or passes a snapshot.
func closeRefusal(p *PRD, in OutcomeInput, now time.Time) string {
	explicit := in.Outcome != "" || p.ClosedOutcome != ""
	effective := in.Outcome
	if effective == "" {
		effective = p.ClosedOutcome
	}
	switch {
	case effective == OutcomeHeld:
		return "its outcome is held: held work is never archived; release the hold, or record another outcome (delivered, superseded or dropped)"
	case holdUnmet(p, now):
		return fmt.Sprintf("it carries a hold that is not met (%s); release the hold first", strings.TrimSpace(p.Hold.Reason))
	case !explicit && !hasShippedRecord(p) && specDeclaresHold(p):
		return "its spec declares a hold, and no outcome is recorded: recreate it as held with its release condition, or record an outcome (delivered, superseded or dropped) to resolve the hold"
	}
	if effective == "" {
		if o, _ := deriveClosedOutcome(p); o == "" {
			return "it has no outcome: record one (delivered, superseded, dropped or held) with the archive or with the outcome action"
		}
	}
	return ""
}

// Archive moves a finished Automaton (completed, rejected or cancelled) to
// archived. The outcome is derived from the record where its facts prove it;
// otherwise the step is refused (autonomous.closed_outcome_archive on) until an
// outcome is recorded.
func (m *Manager) Archive(id, actor string) (*PRD, error) {
	return m.ArchiveWithOutcome(id, actor, OutcomeInput{})
}

// ArchiveWithOutcome is Archive with an explicit outcome recorded in the same
// step. The archive refuses a record with no outcome and one with a hold that
// is not met, and never touches running, planning, paused, held or queued work.
func (m *Manager) ArchiveWithOutcome(id, actor string, in OutcomeInput) (*PRD, error) {
	if actor == "" {
		return nil, fmt.Errorf("archive prd %q: actor is required", id)
	}
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	terminal := map[PRDStatus]bool{
		PRDCompleted: true, PRDRejected: true, PRDCancelled: true, PRDArchived: true,
	}
	if !terminal[prd.Status] {
		return nil, fmt.Errorf("prd %q status %q is not terminal; cancel or complete it first", id, prd.Status)
	}
	in.Outcome, in.DeliveredVia = strings.ToLower(strings.TrimSpace(in.Outcome)), strings.TrimSpace(in.DeliveredVia)
	if err := m.validateOutcomeInput(id, in); err != nil {
		return nil, fmt.Errorf("archive prd %q: %w", id, err)
	}
	if prd.Status == PRDArchived {
		if in.Outcome != "" {
			return m.SetClosedOutcome(id, actor, in)
		}
		return prd, nil // idempotent
	}
	var pendingBranch string
	m.readLocked(func() { pendingBranch = landingPendingBranch(prd) })
	if pendingBranch != "" {
		// The branch may have been merged since the record went pending: the
		// read-only ancestor check flips the record to landed before the
		// refusal, so an already-landed branch archives without the operator
		// touching its landing record.
		if m.CheckLandingAncestor(context.Background(), id) {
			prd, ok = m.store.GetPRD(id)
			if !ok {
				return nil, fmt.Errorf("prd %q not found", id)
			}
		}
		m.readLocked(func() { pendingBranch = landingPendingBranch(prd) })
	}
	if pendingBranch != "" {
		return nil, fmt.Errorf("automaton %q cannot be archived: its branch %s has not landed on the default branch; merge it, or record keep or drop for its landing", id, pendingBranch)
	}
	now := m.holdNow()
	mode := m.Config().EffectiveClosedOutcomeArchive()
	if mode != ClosedOutcomeOff {
		var why string
		m.readLocked(func() { why = closeRefusal(prd, in, now) })
		if why != "" {
			if mode == ClosedOutcomeOn {
				m.recordClosedOutcomeRefusal(id, why)
				return nil, fmt.Errorf("automaton %q cannot be archived: %s: %w", id, why, ErrClosedOutcomeRefused)
			}
			if _, seen := shadowNoted.LoadOrStore(id+"/closed-outcome", true); !seen {
				log.Printf("[autonomous] closed-outcome shadow: automaton %s %s (would be refused)", id, why)
			}
		}
	}
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		m.setOutcomeLocked(prd, in, actor, now, mode)
		prd.Status = PRDArchived
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "archive", Actor: actor})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "archive"), nil)
	// Universal orphan-cleanup backstop: by the time a PRD reaches
	// Archive, the operator has said "done with this forever" —
	// whatever Decision 5's "keep for inspection" grace period on a
	// cancelled/failed/blocked PRD was for has ended. A no-op for a
	// completed PRD (Phase 2 already removed its worktree on success)
	// or one that never had a worktree.
	m.cleanupOrphanedWorktree(prd)
	if m.cleanupArchivedOn() && prd.Git.Branch != "" {
		// An archived Automaton is finished: release its task worktrees, task
		// branches and work branch under the same keep-work rules as at
		// completion and cancel, following cleanup_mode. What is kept now, the
		// periodic pass takes once the work has landed.
		m.ReleaseAutomatonWorktree(prd)
	} else {
		// With cleanup_archived off, archiving removes no task worktree; the
		// result says which ones the Automaton still holds and why.
		m.assessTaskWorktrees(prd)
	}
	updated, _ := m.store.GetPRD(id)
	return updated, nil
}

// setOutcomeLocked records the explicit outcome, or else derives one when the
// record has none, and writes one closed_outcome decision. With the rule off
// only an explicit outcome is recorded. It reports whether the record changed.
// The caller holds the store lock.
func (m *Manager) setOutcomeLocked(p *PRD, in OutcomeInput, actor string, at time.Time, mode string) bool {
	outcome, via, note := in.Outcome, in.DeliveredVia, strings.TrimSpace(in.Note)
	if outcome == "" {
		if p.ClosedOutcome != "" || mode == ClosedOutcomeOff {
			return false
		}
		outcome, note = deriveClosedOutcome(p)
		via = ""
		if outcome == "" {
			return false
		}
	}
	if p.ClosedOutcome == outcome && p.DeliveredVia == via && (note == "" || p.ClosedOutcomeNote == note) {
		return false
	}
	if note == "" {
		note = p.ClosedOutcomeNote
	}
	applyClosedOutcome(p, outcome, via, note, at)
	text := fmt.Sprintf("Outcome recorded: %s", outcome)
	if via != "" {
		text += " via " + via
	}
	if note != "" {
		text += " (" + note + ")"
	}
	p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedOutcome, Actor: actor, Note: text}))
	return true
}

// recordClosedOutcomeRefusal writes the closed_outcome_refused decision once
// per distinct reason, so a sweep retrying every pass does not grow the record.
func (m *Manager) recordClosedOutcomeRefusal(id, why string) {
	note := fmt.Sprintf("Archive refused: %s.", why)
	at := m.holdNow()
	m.store.MutatePRD(id, func(p *PRD) bool {
		if d := lastDecision(p, DecisionClosedOutcomeRefused); d != nil && d.Note == note {
			return false
		}
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedOutcomeRefused, Actor: "autonomous", Note: note}))
		return true
	})
}

// SetClosedOutcome records (or corrects) the outcome of a finished Automaton:
// completed, rejected, cancelled or archived. It never changes the status.
func (m *Manager) SetClosedOutcome(id, actor string, in OutcomeInput) (*PRD, error) {
	if strings.TrimSpace(actor) == "" {
		return nil, fmt.Errorf("outcome of prd %q: actor is required", id)
	}
	in.Outcome, in.DeliveredVia = strings.ToLower(strings.TrimSpace(in.Outcome)), strings.TrimSpace(in.DeliveredVia)
	if in.Outcome == "" {
		return nil, fmt.Errorf("outcome of prd %q: an outcome is required (delivered, superseded, dropped or held)", id)
	}
	prd, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	if !closedWorkStatus(prd.Status) {
		return nil, fmt.Errorf("prd %q is %s: only finished work (completed, rejected, cancelled or archived) has an outcome", id, prd.Status)
	}
	if err := m.validateOutcomeInput(id, in); err != nil {
		return nil, fmt.Errorf("outcome of prd %q: %w", id, err)
	}
	if prd.Status == PRDArchived && in.Outcome == OutcomeHeld {
		return nil, fmt.Errorf("prd %q is archived: held work is never archived; restore it first", id)
	}
	var changed bool
	m.store.WithLock(func() {
		changed = m.setOutcomeLocked(prd, in, actor, m.holdNow(), ClosedOutcomeOn)
		if changed {
			prd.UpdatedAt = m.holdNow()
		}
	})
	if changed {
		if err := m.store.SavePRD(prd); err != nil {
			return nil, err
		}
		m.EmitPRDUpdate(id)
	}
	out, _ := m.store.GetPRD(id)
	return out, nil
}

// CancelWithOutcome is Cancel with an explicit outcome recorded in the same
// step.
func (a *API) CancelWithOutcome(id string, in OutcomeInput) error {
	in.Outcome, in.DeliveredVia = strings.ToLower(strings.TrimSpace(in.Outcome)), strings.TrimSpace(in.DeliveredVia)
	if err := a.M.validateOutcomeInput(id, in); err != nil {
		return fmt.Errorf("cancel prd %q: %w", id, err)
	}
	return a.cancel(id, in)
}
