package planrecords

import (
	"fmt"
	"strings"
	"time"
)

// Audit actions for the two by-hand plan edits. The details carry the old and
// new value and the actor.
const (
	AuditPlanRenamed    = "plan_renamed"
	AuditPlanGoalEdited = "plan_goal_edited"
)

// ValidatePlanTitle checks a plan title the way a rename does: not empty, one
// line, at most MaxPlanTitleRunes characters. The same limit names the plans
// the migration makes, so a plan keeps a title the migration could have given
// it.
func ValidatePlanTitle(title string) error {
	t := strings.TrimSpace(title)
	if NormTitle(t) == "" {
		return invalid("title is required")
	}
	if strings.ContainsAny(t, "\n\r\v\f\u0085  ") {
		return invalid("a plan title is one line")
	}
	if runesOver(t, MaxPlanTitleRunes) {
		return invalid("a plan title is at most %d characters", MaxPlanTitleRunes)
	}
	return nil
}

// resolvePlanLocked resolves ref and requires the record to be a plan.
func (s *Store) resolvePlanLocked(ref string) (Record, error) {
	cur, err := s.resolveLocked(ref, "")
	if err != nil {
		return Record{}, err
	}
	if cur.Kind != KindPlan {
		return Record{}, invalid("%q is a %s, not a plan", cur.Title, cur.Kind)
	}
	return cur, nil
}

// RenamePlan gives the plan ref resolves to a new title. The title is checked
// like a new plan's (ValidatePlanTitle) and must not be used by another record
// in the same project; a clash is refused with *ErrTitleConflict naming that
// record. Renaming a plan to the title it already has changes nothing and is
// not an error. The old title stays as an alias, the title index follows the
// rename, the plan is marked title_set_by_hand so the migration leaves its
// title alone, and an audit event records the old and new title and the actor.
func (s *Store) RenamePlan(ref, newTitle, actor string) (Record, error) {
	t := strings.TrimSpace(newTitle)
	if err := ValidatePlanTitle(t); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.resolvePlanLocked(ref)
	if err != nil {
		return Record{}, err
	}
	if t == cur.Title {
		return cur.clone(), nil
	}
	next := cur.clone()
	if NormTitle(t) != NormTitle(cur.Title) {
		if h, ok := s.byTitle[titleKey(cur.Project, t)]; ok && h != cur.UID {
			return Record{}, &ErrTitleConflict{Title: t, Existing: s.byUID[h].clone()}
		}
		next.Aliases = append(next.Aliases, cur.Title)
	}
	next.Title = t
	next.TitleSetByHand = true
	next.Aliases = dedupeAliases(next.Aliases, next.Title)
	stored, err := s.commitPlanEditLocked(cur, next)
	if err != nil {
		return Record{}, err
	}
	s.audit(AuditPlanRenamed, stored, map[string]any{"old": cur.Title, "new": stored.Title}, actor)
	return stored.clone(), nil
}

// EditPlanGoal sets the goal of the plan ref resolves to. The goal is trimmed
// and may be empty to clear it; it is held to the same length limit as a
// created plan's. Setting the goal it already has changes nothing. An audit
// event records the old and new goal and the actor.
func (s *Store) EditPlanGoal(ref, goal, actor string) (Record, error) {
	g := strings.TrimSpace(goal)
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.resolvePlanLocked(ref)
	if err != nil {
		return Record{}, err
	}
	if g == cur.Goal {
		return cur.clone(), nil
	}
	next := cur.clone()
	next.Goal = g
	stored, err := s.commitPlanEditLocked(cur, next)
	if err != nil {
		return Record{}, err
	}
	s.audit(AuditPlanGoalEdited, stored, map[string]any{"old": cur.Goal, "new": stored.Goal}, actor)
	return stored.clone(), nil
}

// commitPlanEditLocked validates next, stamps updated_at, writes it and swaps
// it into the indexes in place of cur. The caller holds the write lock and has
// already checked the title is free.
func (s *Store) commitPlanEditLocked(cur, next Record) (Record, error) {
	if err := next.validate(); err != nil {
		return Record{}, err
	}
	next.UpdatedAt = s.now().Format(time.RFC3339)
	if err := s.writeFile(next); err != nil {
		return Record{}, fmt.Errorf("planrecords: write %s: %w", next.ID, err)
	}
	if NormTitle(next.Title) != NormTitle(cur.Title) {
		if s.byTitle[titleKey(cur.Project, cur.Title)] == cur.UID {
			delete(s.byTitle, titleKey(cur.Project, cur.Title))
		}
		s.byTitle[titleKey(next.Project, next.Title)] = cur.UID
	}
	stored := next
	s.byUID[cur.UID] = &stored
	s.reindexLinks(&cur, &stored)
	return stored, nil
}
