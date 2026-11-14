package planrecords

import (
	"fmt"
	"sort"
	"strings"
)

// Row links and backlog items.
//
// A plan holds two more kinds of item besides Automata and Automations, both
// kept in the plan's Links list by the uid of the record they name:
//
//   - registry_row: a link from the plan to a registry row record;
//   - backlog_item: a registry row or a finding that has no Automaton yet, held
//     as a backlog item of the plan.
//
// They are items inside a plan, never plans. The store keeps a reverse index in
// step with the plans, so the plans that hold a row can be found from the row.
// A registry row can be linked from more than one plan: the existing link rules
// refuse a second holder only for an Automaton, an Automation and a finding
// attached to a plan, so only a finding already attached to another plan is
// refused as a backlog item (*ErrLinkConflict).

// Link kinds a plan's Links list carries for rows and backlog items. They are
// distinct from the registry-row back link a migrated plan carries.
const (
	LinkKindRegistryRow = "registry_row"
	LinkKindBacklogItem = "backlog_item"
)

func isRowLinkKind(k string) bool { return k == LinkKindRegistryRow || k == LinkKindBacklogItem }

// validateRowLinks checks the row and backlog links of a plan: each is listed
// once, and a record is held either as a row or as a backlog item, not both.
// They apply to plans only; another record's Links list is not looked at.
func (r *Record) validateRowLinks() error {
	if r.Kind != KindPlan {
		return nil
	}
	seen := map[string]string{} // ref -> link kind
	for _, l := range r.Links {
		if !isRowLinkKind(l.Kind) {
			continue
		}
		ref := strings.TrimSpace(l.Ref)
		if prev, dup := seen[ref]; dup {
			if prev == l.Kind {
				return invalid("the plan lists %s %q twice", l.Kind, ref)
			}
			return invalid("%q is held as both %s and %s; it is one or the other", ref, prev, l.Kind)
		}
		seen[ref] = l.Kind
	}
	return nil
}

func hasLink(links []Link, kind, ref string) bool {
	for _, l := range links {
		if l.Kind == kind && strings.TrimSpace(l.Ref) == ref {
			return true
		}
	}
	return false
}

func withoutLink(links []Link, kind, ref string) []Link {
	var out []Link
	for _, l := range links {
		if l.Kind == kind && strings.TrimSpace(l.Ref) == ref {
			continue
		}
		out = append(out, l)
	}
	return out
}

// namesAutomaton reports whether a record already names an Automaton: a link of
// kind automaton, or a finding attached to one.
func namesAutomaton(r Record) bool {
	if r.AttachKind == AttachAutomaton {
		return true
	}
	for _, l := range r.Links {
		if l.Kind == linkAutomaton && strings.TrimSpace(l.Ref) != "" {
			return true
		}
	}
	return false
}

// AttachRow links a registry row to a plan, by the row's uid, and returns the
// plan. Linking the same row again changes nothing. A row may be linked from
// several plans. A row already held by this plan as a backlog item is refused.
func (s *Store) AttachRow(planRef, rowRef string) (Record, error) {
	return s.holdRow(LinkKindRegistryRow, planRef, rowRef)
}

// AddBacklogItem holds a registry row or a finding that has no Automaton as a
// backlog item of the plan, by its uid, and returns the plan. Holding the same
// item again changes nothing. A row or finding that already names an Automaton
// is refused, as is a finding already attached to a different plan
// (*ErrLinkConflict). A registry row held as a backlog item by another plan is
// allowed, because no link rule refuses it.
func (s *Store) AddBacklogItem(planRef, itemRef string) (Record, error) {
	return s.holdRow(LinkKindBacklogItem, planRef, itemRef)
}

func (s *Store) holdRow(kind, planRef, itemRef string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	item, err := s.resolveLocked(itemRef, "")
	if err != nil {
		return Record{}, err
	}
	switch {
	case kind == LinkKindRegistryRow && item.Kind != KindRegistryRow:
		return Record{}, invalid("%q is a %s, not a registry row", itemRef, item.Kind)
	case kind == LinkKindBacklogItem && item.Kind != KindRegistryRow && item.Kind != KindFinding:
		return Record{}, invalid("%q is a %s; a backlog item is a registry row or a finding", itemRef, item.Kind)
	}
	if kind == LinkKindBacklogItem {
		if namesAutomaton(item) {
			return Record{}, invalid("%q already has an Automaton, so it is not a backlog item", item.ID)
		}
		if item.Kind == KindFinding && item.AttachKind != "" && !(item.AttachKind == AttachPlan && item.AttachRef == plan.ID) {
			return Record{}, &ErrLinkConflict{Kind: "finding", ID: item.ID, Holder: item.AttachKind + ":" + item.AttachRef}
		}
	}
	if hasLink(plan.Links, kind, item.UID) {
		return plan.clone(), nil
	}
	other := LinkKindBacklogItem
	if kind == LinkKindBacklogItem {
		other = LinkKindRegistryRow
	}
	if hasLink(plan.Links, other, item.UID) {
		return Record{}, invalid("%q is already held by plan %q as %s; remove that link first", item.ID, plan.ID, other)
	}
	next := plan.clone()
	next.Links = append(next.Links, Link{Kind: kind, Ref: item.UID})
	if err := next.validate(); err != nil {
		return Record{}, err
	}
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditLinked, next, map[string]any{"link": kind, "link_id": item.UID, "item_id": item.ID}, "")
	return next.clone(), nil
}

// DetachRow removes a registry row link from a plan and returns the plan. A row
// the plan does not link is ErrNotFound. The row itself is untouched.
func (s *Store) DetachRow(planRef, rowRef string) (Record, error) {
	return s.releaseRow(LinkKindRegistryRow, planRef, rowRef)
}

// RemoveBacklogItem removes a backlog item from a plan and returns the plan. An
// item the plan does not hold is ErrNotFound. The item itself is untouched.
func (s *Store) RemoveBacklogItem(planRef, itemRef string) (Record, error) {
	return s.releaseRow(LinkKindBacklogItem, planRef, itemRef)
}

func (s *Store) releaseRow(kind, planRef, itemRef string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	item, err := s.resolveLocked(itemRef, "")
	if err != nil {
		return Record{}, err
	}
	if !hasLink(plan.Links, kind, item.UID) {
		return Record{}, fmt.Errorf("%w: %s %q is not held by plan %q", ErrNotFound, kind, item.ID, plan.ID)
	}
	next := plan.clone()
	next.Links = withoutLink(next.Links, kind, item.UID)
	if len(next.Links) == 0 {
		next.Links = nil
	}
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditUnlinked, next, map[string]any{"link": kind, "link_id": item.UID, "item_id": item.ID}, "")
	return next.clone(), nil
}

// RowItem is one registry row or backlog item a plan holds, as the plan lists
// it, read from the summary index.
type RowItem struct {
	// Link is LinkKindRegistryRow or LinkKindBacklogItem.
	Link         string `json:"link"`
	UID          string `json:"uid"`
	ID           string `json:"id,omitempty"`
	Kind         Kind   `json:"kind,omitempty"`
	Project      string `json:"project,omitempty"`
	Title        string `json:"title,omitempty"`
	State        State  `json:"state,omitempty"`
	LegacyNumber int    `json:"legacy_number,omitempty"`
}

// PlanRows is a plan's registry rows and backlog items, each in the order they
// were added.
type PlanRows struct {
	Plan    PlanRef   `json:"plan"`
	Rows    []RowItem `json:"rows"`
	Backlog []RowItem `json:"backlog"`
}

func (s *Store) rowItemLocked(link, uid string) RowItem {
	it := RowItem{Link: link, UID: uid}
	if sm, ok := s.li.sums[uid]; ok {
		it.ID, it.Kind, it.Project, it.Title, it.State, it.LegacyNumber = sm.ID, sm.Kind, sm.Project, sm.Title, sm.State, sm.LegacyNumber
	}
	return it
}

// RowsOf lists the registry rows and the backlog items a plan holds.
func (s *Store) RowsOf(planRef string) (PlanRows, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return PlanRows{}, err
	}
	pr, _ := s.planRefLocked(plan.UID)
	out := PlanRows{Plan: pr, Rows: []RowItem{}, Backlog: []RowItem{}}
	for _, l := range plan.Links {
		switch l.Kind {
		case LinkKindRegistryRow:
			out.Rows = append(out.Rows, s.rowItemLocked(l.Kind, strings.TrimSpace(l.Ref)))
		case LinkKindBacklogItem:
			out.Backlog = append(out.Backlog, s.rowItemLocked(l.Kind, strings.TrimSpace(l.Ref)))
		}
	}
	return out, nil
}

// PlansOfRow lists the plans that link a registry row, in priority order, from
// the link index. The reference is anything Resolve accepts.
func (s *Store) PlansOfRow(rowRef string) ([]PlanRef, error) {
	return s.plansHolding(LinkKindRegistryRow, rowRef)
}

// PlansOfBacklogItem lists the plans that hold a registry row or finding as a
// backlog item, in priority order, from the link index.
func (s *Store) PlansOfBacklogItem(itemRef string) ([]PlanRef, error) {
	return s.plansHolding(LinkKindBacklogItem, itemRef)
}

func (s *Store) plansHolding(kind, ref string) ([]PlanRef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, err := s.resolveLocked(ref, "")
	if err != nil {
		return nil, err
	}
	var sums []Summary
	for _, uid := range s.li.rowHolders[linkKey(kind, item.UID)] {
		if sm, ok := s.li.sums[uid]; ok {
			sums = append(sums, sm)
		}
	}
	sort.Slice(sums, func(i, j int) bool { return planLess(sums[i], sums[j]) })
	out := make([]PlanRef, 0, len(sums))
	for _, sm := range sums {
		out = append(out, PlanRef{UID: sm.UID, ID: sm.ID, Title: sm.Title, State: sm.State, Order: sm.Order})
	}
	return out, nil
}
