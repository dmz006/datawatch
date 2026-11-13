package planrecords

import (
	"fmt"
	"sort"
	"strings"
)

// RollupActor is the actor on the audit line of a roll-up write.
const RollupActor = "rollup"

// AuditRolledUp is the audit action of a roll-up write.
const AuditRolledUp = "plan_record_rolled_up"

// RollupChange is one plan the roll-up looked at and found different from
// its derived status. SetByHandKept is true when the plan's status was set by
// hand: To is then what the roll-up would have written, and nothing was.
type RollupChange struct {
	Title         string `json:"title"`
	Plan          string `json:"plan"`
	From          State  `json:"from"`
	To            State  `json:"to"`
	Reason        string `json:"reason"`
	SetByHandKept bool   `json:"set_by_hand_kept,omitempty"`
}

// DerivePlanStatus derives a plan's status from the states last reported for
// the Automata and Automations linked to it. It is pure.
//
//   - no linked work: open
//   - any failed or blocked: blocked (it wins over running, because work
//     that cannot go on is what the operator has to look at)
//   - any running: open
//   - all finished: shipped when every one is shipped, otherwise done
//   - anything else (some work still waiting its turn or held): open
//
// The second return value is a short reason for the audit line.
func DerivePlanStatus(states []string) (State, string) {
	if len(states) == 0 {
		return StateOpen, "no linked work"
	}
	var blocked, running, shipped, finished int
	for _, st := range states {
		switch RunClass(st) {
		case RunBlocked:
			blocked++
		case RunRunning:
			running++
		case RunCompleted:
			finished++
			if strings.ToLower(strings.TrimSpace(st)) == "shipped" {
				shipped++
			}
		}
	}
	switch {
	case blocked > 0:
		return StateBlocked, fmt.Sprintf("%d of %d linked items failed or blocked", blocked, len(states))
	case running > 0:
		return StateOpen, fmt.Sprintf("%d of %d linked items running", running, len(states))
	case finished == len(states) && shipped == len(states):
		return StateShipped, fmt.Sprintf("all %d linked items shipped", len(states))
	case finished == len(states):
		return StateDone, fmt.Sprintf("all %d linked items finished", len(states))
	}
	return StateOpen, fmt.Sprintf("%d of %d linked items finished", finished, len(states))
}

// linkedStates lists the last reported state of each Automaton and Automation
// linked to a plan.
func linkedStates(p Record) []string {
	var out []string
	for _, id := range p.Automata {
		out = append(out, p.LinkInfo[linkKey(linkAutomaton, id)].State)
	}
	for _, id := range p.Automations {
		out = append(out, p.LinkInfo[linkKey(linkAutomation, id)].State)
	}
	return out
}

// rollupLocked works out the change for one plan (nil when none is needed)
// and, unless dryRun, writes it. The caller holds the write lock.
func (s *Store) rollupLocked(plan Record, dryRun bool) (*RollupChange, error) {
	states := linkedStates(plan)
	if len(states) == 0 {
		// Nothing is linked, so there is nothing to roll up: the plan keeps
		// whatever state it has.
		return nil, nil
	}
	to, reason := DerivePlanStatus(states)
	if to == plan.State {
		return nil, nil
	}
	ch := &RollupChange{Title: plan.Title, Plan: plan.ID, From: plan.State, To: to, Reason: reason}
	if plan.SetByHand {
		ch.SetByHandKept = true
		return ch, nil
	}
	if dryRun {
		return ch, nil
	}
	next := plan.clone()
	next.State = to
	s.touch(&next)
	switch {
	case IsClosed(next.Kind, next.State) && !IsClosed(plan.Kind, plan.State):
		next.ClosedAt = next.UpdatedAt
	case !IsClosed(next.Kind, next.State):
		next.ClosedAt = ""
	}
	if err := s.commit(next); err != nil {
		return nil, err
	}
	s.audit(AuditRolledUp, next, map[string]any{
		"changed": []string{"state"}, "state": string(to), "from": string(ch.From), "reason": reason,
	}, RollupActor)
	return ch, nil
}

// RollupPlan derives the status of one plan from its linked work and, unless
// dryRun, writes it. It returns nil when the plan already has that status or
// has no linked work. A status set by hand is kept and reported.
func (s *Store) RollupPlan(ref string, dryRun bool) (*RollupChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planLocked(ref)
	if err != nil {
		return nil, err
	}
	return s.rollupLocked(plan, dryRun)
}

// ReconcilePlans rolls up every plan of a project (every project when it is
// empty) and returns what differs, in plan-id order.
func (s *Store) ReconcilePlans(project string, dryRun bool) ([]RollupChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var plans []Record
	for _, r := range s.byUID {
		if r.Kind == KindPlan && (project == "" || r.Project == project) {
			plans = append(plans, *r)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	out := []RollupChange{}
	for _, p := range plans {
		ch, err := s.rollupLocked(p, dryRun)
		if err != nil {
			return out, err
		}
		if ch != nil {
			out = append(out, *ch)
		}
	}
	return out, nil
}

// NoteLinkState records the state a linked Automaton or Automation just
// reported (keeping the title already held when info has none) and rolls the
// plan it belongs to up. A kind or id that is not linked to a plan is
// ErrNotFound and changes nothing.
func (s *Store) NoteLinkState(kind, rawID string, info LinkInfo) (*RollupChange, error) {
	id, err := cleanLinkID(rawID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.li.linked[linkKey(kind, id)]
	if !ok {
		return nil, fmt.Errorf("%w: %s %q is not linked to a plan", ErrNotFound, kind, id)
	}
	plan := *s.byUID[e.planUID]
	k := linkKey(kind, id)
	cur := plan.LinkInfo[k]
	if info.Title == "" {
		info.Title = cur.Title
	}
	if info.CompletedAt == "" {
		info.CompletedAt = cur.CompletedAt
	}
	if info != cur {
		next := plan.clone()
		if next.LinkInfo == nil {
			next.LinkInfo = map[string]LinkInfo{}
		}
		next.LinkInfo[k] = info
		if err := next.validate(); err != nil {
			return nil, err
		}
		s.touch(&next)
		if err := s.commit(next); err != nil {
			return nil, err
		}
		plan = next
	}
	return s.rollupLocked(plan, false)
}

// Link kinds for NoteLinkState.
const (
	LinkKindAutomaton  = linkAutomaton
	LinkKindAutomation = linkAutomation
)
