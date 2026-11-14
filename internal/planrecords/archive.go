package planrecords

import (
	"fmt"
	"strings"
)

// Archive and restore of a plan. Archiving takes a plan out of the default
// plan lists, the backlog, search results and the project overview counts. It
// is a mark on the plan record and nothing else: the plan's file stays on disk,
// every link it holds (Automata, Automations, findings, registry rows, themes)
// stays in the index, and no Automaton, Automation, registry row, finding or
// config key is touched. Restore clears the mark; the plan comes back in the
// place its order gave it.

// Audit actions for archive and restore. They are emitted once per real change.
const (
	AuditArchived = "plan_record_archived"
	AuditRestored = "plan_record_restored"
)

// ArchiveScope says which plans a list, search or count covers. The zero value
// is the default: archived plans are left out.
type ArchiveScope struct {
	// IncludeArchived adds archived plans to the result.
	IncludeArchived bool
	// ArchivedOnly returns archived plans only; it wins over IncludeArchived.
	ArchivedOnly bool
}

// allows reports whether a plan with the given archived mark is in scope.
func (a ArchiveScope) allows(archived bool) bool {
	switch {
	case a.ArchivedOnly:
		return archived
	case a.IncludeArchived:
		return true
	}
	return !archived
}

// ErrProtectedPlan is returned when Archive is asked to archive the plan the
// caller named as protected while that plan holds Automata.
type ErrProtectedPlan struct {
	Plan     Record
	Automata int
}

func (e *ErrProtectedPlan) Error() string {
	return fmt.Sprintf("planrecords: plan %q (%s) is protected and holds %d Automata; it cannot be archived",
		e.Plan.Title, e.Plan.ID, e.Automata)
}

// ArchiveOption adjusts one Archive call.
type ArchiveOption func(*archiveConfig)

type archiveConfig struct {
	protected string
}

// WithProtectedPlan names the one plan (usually the current release plan) that
// must not be archived while it holds Automata. The reference is resolved like
// any other plan reference; one that names no plan fails the call.
func WithProtectedPlan(ref string) ArchiveOption {
	return func(c *archiveConfig) { c.protected = strings.TrimSpace(ref) }
}

// ArchiveResult is what Archive reports.
type ArchiveResult struct {
	// Plan is the plan after the call.
	Plan Record
	// AutomataHeld is how many Automata the plan holds. They stay linked.
	AutomataHeld int
	// Changed is false when the plan was already archived and nothing was written.
	Changed bool
}

// Archive marks the plan a reference resolves to as archived, with an optional
// reason of at most MaxArchiveReasonRunes characters (longer is rejected, not
// cut). Archiving an archived plan changes nothing and returns it unchanged. A
// plan that holds Automata is refused with *ErrProtectedPlan when it is the
// plan named by WithProtectedPlan. One audit event (AuditArchived) is emitted
// per real change, carrying the actor, the reason and the Automata count.
func (s *Store) Archive(ref, reason, actor string, opts ...ArchiveOption) (ArchiveResult, error) {
	var cfg archiveConfig
	for _, o := range opts {
		o(&cfg)
	}
	reason = strings.TrimSpace(reason)
	if runesOver(reason, MaxArchiveReasonRunes) {
		return ArchiveResult{}, invalid("archive reason longer than %d characters", MaxArchiveReasonRunes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.planLocked(ref)
	if err != nil {
		return ArchiveResult{}, err
	}
	held := len(cur.Automata)
	if cur.Archived {
		return ArchiveResult{Plan: cur.clone(), AutomataHeld: held}, nil
	}
	if cfg.protected != "" {
		prot, err := s.planLocked(cfg.protected)
		if err != nil {
			return ArchiveResult{}, fmt.Errorf("protected plan: %w", err)
		}
		if held > 0 && prot.UID == cur.UID {
			return ArchiveResult{}, &ErrProtectedPlan{Plan: cur.clone(), Automata: held}
		}
	}
	next := cur.clone()
	s.touch(&next)
	next.Archived, next.ArchivedAt, next.ArchiveReason = true, next.UpdatedAt, reason
	if err := next.validate(); err != nil {
		return ArchiveResult{}, err
	}
	if err := s.commit(next); err != nil {
		return ArchiveResult{}, err
	}
	s.audit(AuditArchived, next, map[string]any{"reason": reason, "automata_held": held, "order": next.Order}, actor)
	return ArchiveResult{Plan: next.clone(), AutomataHeld: held, Changed: true}, nil
}

// Restore clears the archive mark of the plan a reference resolves to. The plan
// keeps the order it had, so it returns to its old place in the lists.
// Restoring a plan that is not archived changes nothing and returns it
// unchanged. One audit event (AuditRestored) is emitted per real change.
func (s *Store) Restore(ref, actor string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.planLocked(ref)
	if err != nil {
		return Record{}, err
	}
	if !cur.Archived {
		return cur.clone(), nil
	}
	next := cur.clone()
	next.Archived, next.ArchivedAt, next.ArchiveReason = false, "", ""
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditRestored, next, map[string]any{"automata_held": len(next.Automata), "order": next.Order}, actor)
	return next.clone(), nil
}
