package planrecords

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Audit actions for link and order changes.
const (
	AuditLinked    = "plan_record_linked"
	AuditUnlinked  = "plan_record_unlinked"
	AuditReordered = "plan_record_reordered"
)

// Link kinds used in the reverse index and in LinkInfo keys.
const (
	linkAutomaton  = "automaton"
	linkAutomation = "automation"
)

// ErrLinkConflict is returned when an attach would overwrite an existing
// link: the Automaton or Automation is already linked to a different plan, or
// the finding is already attached somewhere else. A link is never replaced;
// detach it first.
type ErrLinkConflict struct {
	// Kind is "automaton", "automation" or "finding".
	Kind string
	// ID is the item that was being attached.
	ID string
	// Holder is where it is attached now: a plan id for an Automaton or
	// Automation, "plan:<id>" or "automaton:<id>" for a finding.
	Holder string
}

func (e *ErrLinkConflict) Error() string {
	return fmt.Sprintf("planrecords: %s %q is already linked to %s; detach it first", e.Kind, e.ID, e.Holder)
}

// Target is what a finding attaches to.
type Target struct {
	// Kind is AttachPlan or AttachAutomaton.
	Kind string
	// Ref is a plan reference or an Automaton id.
	Ref string
}

// Summary is the light form of a record the list and search paths read. It is
// kept in step with the records on every write, so a list never reads a full
// record.
type Summary struct {
	UID     string `json:"uid"`
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Project string `json:"project"`
	State   State  `json:"state"`
	// SetByHand is true when the plan's status was written by hand, so the
	// roll-up keeps it (kind plan only).
	SetByHand    bool   `json:"set_by_hand,omitempty"`
	Title        string `json:"title"`
	Order        int    `json:"order,omitempty"`
	LegacyNumber int    `json:"legacy_number,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	// Plan counts (kind plan only).
	AutomatonCount  int `json:"automaton_count,omitempty"`
	AutomationCount int `json:"automation_count,omitempty"`
	FindingCount    int `json:"finding_count,omitempty"`
	// AttachKind and AttachRef say what a finding is attached to.
	AttachKind string `json:"attach_kind,omitempty"`
	AttachRef  string `json:"attach_ref,omitempty"`
	// Goal is the plan's goal (kind plan only), clipped to MaxGoalRunes.
	Goal string `json:"goal,omitempty"`
	// LinkedAutomata are the Automaton ids a registry row or finding links to.
	LinkedAutomata []string `json:"linked_automata,omitempty"`
}

// PlanRef names the plan an Automaton or Automation belongs to.
type PlanRef struct {
	UID   string `json:"uid"`
	ID    string `json:"id"`
	Title string `json:"title"`
	State State  `json:"state"`
	Order int    `json:"order,omitempty"`
}

// AutomatonLink is one linked Automaton as the plan lists it.
type AutomatonLink struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	State string `json:"state,omitempty"`
}

type linkEntry struct {
	planUID string
	info    LinkInfo
}

// linkIndex is the summary index and the two-way link lookups. The store
// mutex guards it.
type linkIndex struct {
	sums map[string]Summary // uid -> summary
	// linked maps "automaton:<id>" / "automation:<id>" to the owning plan.
	linked       map[string]linkEntry
	planAutomata map[string][]string // plan uid -> linked Automaton ids, in order
	// planAutomations maps a plan uid to its linked Automation ids, in order.
	planAutomations map[string][]string
	// rowPlan maps a registry row id to the uid of the plan made from it.
	rowPlan map[string]string
}

func newLinkIndex() linkIndex {
	return linkIndex{
		sums:            map[string]Summary{},
		linked:          map[string]linkEntry{},
		planAutomata:    map[string][]string{},
		planAutomations: map[string][]string{},
		rowPlan:         map[string]string{},
	}
}

func linkKey(kind, id string) string { return kind + ":" + id }

func summarize(r *Record) Summary {
	sm := summarizeBase(r)
	sm.Goal = r.Goal
	if r.Kind != KindPlan {
		for _, l := range r.Links {
			if l.Kind == linkAutomaton && strings.TrimSpace(l.Ref) != "" {
				sm.LinkedAutomata = append(sm.LinkedAutomata, strings.TrimSpace(l.Ref))
			}
		}
	}
	return sm
}

func summarizeBase(r *Record) Summary {
	return Summary{
		UID: r.UID, ID: r.ID, Kind: r.Kind, Project: r.Project, State: r.State, SetByHand: r.SetByHand, Title: r.Title,
		Order: r.Order, LegacyNumber: r.LegacyNumber, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		AutomatonCount: len(r.Automata), AutomationCount: len(r.Automations), FindingCount: len(r.Findings),
		AttachKind: r.AttachKind, AttachRef: r.AttachRef,
	}
}

// reindexLinks swaps old for next in the summary index and the reverse
// lookups. old is nil for a new record. The caller holds the write lock (or
// is Open).
func (s *Store) reindexLinks(old, next *Record) {
	li := &s.li
	if old != nil {
		delete(li.sums, old.UID)
		if old.Kind == KindPlan {
			for _, id := range old.Automata {
				if e, ok := li.linked[linkKey(linkAutomaton, id)]; ok && e.planUID == old.UID {
					delete(li.linked, linkKey(linkAutomaton, id))
				}
			}
			for _, id := range old.Automations {
				if e, ok := li.linked[linkKey(linkAutomation, id)]; ok && e.planUID == old.UID {
					delete(li.linked, linkKey(linkAutomation, id))
				}
			}
			delete(li.planAutomata, old.UID)
			delete(li.planAutomations, old.UID)
			for _, l := range old.Links {
				if l.Kind == linkRegistryRow && li.rowPlan[l.Ref] == old.UID {
					delete(li.rowPlan, l.Ref)
				}
			}
		}
	}
	li.sums[next.UID] = summarize(next)
	if next.Kind != KindPlan {
		return
	}
	add := func(kind string, ids []string) []string {
		var kept []string
		for _, id := range ids {
			k := linkKey(kind, id)
			if e, ok := li.linked[k]; ok && e.planUID != next.UID {
				s.logf("planrecords: plan %s lists %s %q already linked to another plan; link ignored", next.ID, kind, id)
				continue
			}
			li.linked[k] = linkEntry{planUID: next.UID, info: next.LinkInfo[k]}
			kept = append(kept, id)
		}
		return kept
	}
	li.planAutomata[next.UID] = add(linkAutomaton, next.Automata)
	li.planAutomations[next.UID] = add(linkAutomation, next.Automations)
	for _, l := range next.Links {
		if l.Kind == linkRegistryRow && l.Ref != "" {
			li.rowPlan[l.Ref] = next.UID
		}
	}
}

// planOrderKey sorts plans: lowest order first, an unset order last, then
// oldest first, then id.
func planLess(a, b Summary) bool {
	ao, bo := a.Order, b.Order
	if ao == 0 {
		ao = MaxOrder + 1
	}
	if bo == 0 {
		bo = MaxOrder + 1
	}
	if ao != bo {
		return ao < bo
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt < b.CreatedAt
	}
	return a.ID < b.ID
}

func (s *Store) planSummariesLocked() []Summary {
	var out []Summary
	for _, sm := range s.li.sums {
		if sm.Kind == KindPlan {
			out = append(out, sm)
		}
	}
	sort.Slice(out, func(i, j int) bool { return planLess(out[i], out[j]) })
	return out
}

// Plans lists the plans in priority order, from the summary index. A
// non-empty state keeps only plans in that state.
func (s *Store) Plans(state State) []Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Summary
	for _, sm := range s.planSummariesLocked() {
		if state == "" || sm.State == state {
			out = append(out, sm)
		}
	}
	return out
}

// Backlog lists the plans that have no Automaton yet, in priority order.
func (s *Store) Backlog() []Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Summary
	for _, sm := range s.planSummariesLocked() {
		if sm.AutomatonCount == 0 {
			out = append(out, sm)
		}
	}
	return out
}

// SummaryFilter narrows ListSummaries. Zero fields match everything.
type SummaryFilter struct {
	Kind    Kind
	Project string
	State   State
	Limit   int
	Offset  int
}

func (f SummaryFilter) matches(sm Summary) bool {
	return (f.Kind == "" || sm.Kind == f.Kind) && (f.Project == "" || sm.Project == f.Project) &&
		(f.State == "" || sm.State == f.State)
}

// ListSummaries returns one page of summaries and the total before paging.
// Plans come in priority order; any other selection is newest-updated first.
// It reads the summary index only.
func (s *Store) ListSummaries(f SummaryFilter) ([]Summary, int) {
	s.mu.RLock()
	var all []Summary
	for _, sm := range s.li.sums {
		if f.matches(sm) {
			all = append(all, sm)
		}
	}
	s.mu.RUnlock()
	if f.Kind == KindPlan {
		sort.Slice(all, func(i, j int) bool { return planLess(all[i], all[j]) })
	} else {
		sort.Slice(all, func(i, j int) bool {
			if all[i].UpdatedAt != all[j].UpdatedAt {
				return all[i].UpdatedAt > all[j].UpdatedAt
			}
			return all[i].ID < all[j].ID
		})
	}
	lo, hi := Filter{Limit: f.Limit, Offset: f.Offset}.page(len(all))
	return append([]Summary(nil), all[lo:hi]...), len(all)
}

func (s *Store) planRefLocked(uid string) (PlanRef, bool) {
	sm, ok := s.li.sums[uid]
	if !ok {
		return PlanRef{}, false
	}
	return PlanRef{UID: sm.UID, ID: sm.ID, Title: sm.Title, State: sm.State, Order: sm.Order}, true
}

// PlanOf returns the plan an Automaton is linked to, from the index only.
func (s *Store) PlanOf(automatonID string) (PlanRef, bool) {
	return s.planOf(linkAutomaton, automatonID)
}

// PlanOfAutomation returns the plan an Automation is linked to.
func (s *Store) PlanOfAutomation(automationID string) (PlanRef, bool) {
	return s.planOf(linkAutomation, automationID)
}

func (s *Store) planOf(kind, id string) (PlanRef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.li.linked[linkKey(kind, strings.TrimSpace(id))]
	if !ok {
		return PlanRef{}, false
	}
	return s.planRefLocked(e.planUID)
}

// AutomataOf lists a plan's Automata in order, from the index only.
func (s *Store) AutomataOf(planRef string) ([]AutomatonLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return nil, err
	}
	out := []AutomatonLink{}
	for _, id := range s.li.planAutomata[plan.UID] {
		info := s.li.linked[linkKey(linkAutomaton, id)].info
		out = append(out, AutomatonLink{ID: id, Title: info.Title, State: info.State})
	}
	return out, nil
}

// planLocked resolves a reference that must name a plan.
func (s *Store) planLocked(ref string) (Record, error) {
	r, err := s.resolveLocked(ref, "")
	if err != nil {
		return Record{}, err
	}
	if r.Kind != KindPlan {
		return Record{}, invalid("%q is a %s, not a plan", ref, r.Kind)
	}
	return r, nil
}

// commit writes the new versions of one or more records, then swaps them into
// the index. If a write fails, the files already written are put back.
func (s *Store) commit(next ...Record) error {
	olds := make([]*Record, len(next))
	for i := range next {
		olds[i] = s.byUID[next[i].UID]
	}
	for i := range next {
		if err := s.writeFile(next[i]); err != nil {
			for j := 0; j < i; j++ {
				if olds[j] != nil {
					_ = s.writeFile(*olds[j])
				}
			}
			return fmt.Errorf("planrecords: write %s: %w", next[i].ID, err)
		}
	}
	for i := range next {
		stored := next[i]
		stored.Actor = ""
		s.byUID[stored.UID] = &stored
		s.reindexLinks(olds[i], &stored)
	}
	return nil
}

func (s *Store) touch(r *Record) { r.UpdatedAt = s.now().Format(time.RFC3339) }

func cleanLinkID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || runesOver(id, MaxLinkRefRunes) {
		return "", invalid("an id must be non-empty and at most %d characters", MaxLinkRefRunes)
	}
	return id, nil
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

func removeID(ids []string, id string) []string {
	var out []string
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

// AttachAutomaton links an Automaton to a plan, last in its order. info is the
// Automaton's title and state as the caller last saw them; attaching the same
// Automaton to the same plan again only refreshes it. An Automaton already
// linked to a different plan returns *ErrLinkConflict and nothing changes.
func (s *Store) AttachAutomaton(planRef, automatonID string, info LinkInfo) (Record, error) {
	return s.attach(linkAutomaton, planRef, automatonID, info)
}

// AttachAutomation links an Automation to a plan; see AttachAutomaton.
func (s *Store) AttachAutomation(planRef, automationID string, info LinkInfo) (Record, error) {
	return s.attach(linkAutomation, planRef, automationID, info)
}

func (s *Store) attach(kind, planRef, rawID string, info LinkInfo) (Record, error) {
	id, err := cleanLinkID(rawID)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	if e, ok := s.li.linked[linkKey(kind, id)]; ok && e.planUID != plan.UID {
		holder := e.planUID
		if p, ok := s.planRefLocked(e.planUID); ok {
			holder = p.ID
		}
		return Record{}, &ErrLinkConflict{Kind: kind, ID: id, Holder: holder}
	}
	next := plan.clone()
	list := &next.Automata
	if kind == linkAutomation {
		list = &next.Automations
	}
	k := linkKey(kind, id)
	already := indexOf(*list, id) >= 0
	if !already {
		*list = append(*list, id)
	}
	if next.LinkInfo == nil {
		next.LinkInfo = map[string]LinkInfo{}
	}
	if info != (LinkInfo{}) {
		next.LinkInfo[k] = info
	}
	if already && next.LinkInfo[k] == plan.LinkInfo[k] {
		return plan.clone(), nil
	}
	if len(next.LinkInfo) == 0 {
		next.LinkInfo = nil
	}
	if err := next.validate(); err != nil {
		return Record{}, err
	}
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditLinked, next, map[string]any{"link": kind, "link_id": id}, "")
	return next.clone(), nil
}

// DetachAutomaton removes an Automaton from the plan it is linked to and
// returns that plan. An Automaton that is not linked is ErrNotFound.
func (s *Store) DetachAutomaton(automatonID string) (Record, error) {
	return s.detach(linkAutomaton, automatonID)
}

// DetachAutomation removes an Automation from its plan; see DetachAutomaton.
func (s *Store) DetachAutomation(automationID string) (Record, error) {
	return s.detach(linkAutomation, automationID)
}

func (s *Store) detach(kind, rawID string) (Record, error) {
	id, err := cleanLinkID(rawID)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.li.linked[linkKey(kind, id)]
	if !ok {
		return Record{}, fmt.Errorf("%w: %s %q is not linked to a plan", ErrNotFound, kind, id)
	}
	plan := *s.byUID[e.planUID]
	next := plan.clone()
	if kind == linkAutomaton {
		next.Automata = removeID(next.Automata, id)
	} else {
		next.Automations = removeID(next.Automations, id)
	}
	delete(next.LinkInfo, linkKey(kind, id))
	if len(next.LinkInfo) == 0 {
		next.LinkInfo = nil
	}
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditUnlinked, next, map[string]any{"link": kind, "link_id": id}, "")
	return next.clone(), nil
}

// AttachFinding attaches a finding to a plan or to an Automaton and returns
// the finding. A finding already attached to something else returns
// *ErrLinkConflict; attaching it to the same target again changes nothing.
func (s *Store) AttachFinding(findingRef string, to Target) (Record, error) {
	if to.Kind != AttachPlan && to.Kind != AttachAutomaton {
		return Record{}, invalid("a finding attaches to a plan or an Automaton")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fin, err := s.resolveLocked(findingRef, "")
	if err != nil {
		return Record{}, err
	}
	if fin.Kind != KindFinding {
		return Record{}, invalid("%q is a %s, not a finding", findingRef, fin.Kind)
	}
	var plan Record
	ref := ""
	if to.Kind == AttachPlan {
		if plan, err = s.planLocked(to.Ref); err != nil {
			return Record{}, err
		}
		ref = plan.ID
	} else if ref, err = cleanLinkID(to.Ref); err != nil {
		return Record{}, err
	}
	if fin.AttachKind != "" {
		if fin.AttachKind == to.Kind && fin.AttachRef == ref {
			return fin.clone(), nil
		}
		return Record{}, &ErrLinkConflict{Kind: "finding", ID: fin.ID, Holder: fin.AttachKind + ":" + fin.AttachRef}
	}
	nextFin := fin.clone()
	nextFin.AttachKind, nextFin.AttachRef = to.Kind, ref
	s.touch(&nextFin)
	recs := []Record{nextFin}
	if to.Kind == AttachPlan {
		nextPlan := plan.clone()
		if indexOf(nextPlan.Findings, fin.ID) < 0 {
			nextPlan.Findings = append(nextPlan.Findings, fin.ID)
		}
		if err := nextPlan.validate(); err != nil {
			return Record{}, err
		}
		s.touch(&nextPlan)
		recs = append(recs, nextPlan)
	}
	if err := s.commit(recs...); err != nil {
		return Record{}, err
	}
	s.audit(AuditLinked, nextFin, map[string]any{"link": "finding", "attach_kind": to.Kind, "attach_ref": ref}, "")
	return nextFin.clone(), nil
}

// DetachFinding clears a finding's attachment, and removes it from the plan's
// findings when it was attached to a plan. It returns the finding.
func (s *Store) DetachFinding(findingRef string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fin, err := s.resolveLocked(findingRef, "")
	if err != nil {
		return Record{}, err
	}
	if fin.Kind != KindFinding {
		return Record{}, invalid("%q is a %s, not a finding", findingRef, fin.Kind)
	}
	if fin.AttachKind == "" {
		return Record{}, fmt.Errorf("%w: finding %q is not attached", ErrNotFound, fin.ID)
	}
	nextFin := fin.clone()
	nextFin.AttachKind, nextFin.AttachRef = "", ""
	s.touch(&nextFin)
	recs := []Record{nextFin}
	if fin.AttachKind == AttachPlan {
		if uid, ok := s.byID[fin.AttachRef]; ok && s.byUID[uid].Kind == KindPlan {
			nextPlan := s.byUID[uid].clone()
			nextPlan.Findings = removeID(nextPlan.Findings, fin.ID)
			s.touch(&nextPlan)
			recs = append(recs, nextPlan)
		}
	}
	if err := s.commit(recs...); err != nil {
		return Record{}, err
	}
	s.audit(AuditUnlinked, nextFin, map[string]any{"link": "finding", "attach_kind": fin.AttachKind, "attach_ref": fin.AttachRef}, "")
	return nextFin.clone(), nil
}

// ReorderPlans sets the priority order of plans. The listed plans take
// positions 1..n in the order given; every other plan keeps its relative
// order after them. A reference that is not a plan, or a plan listed twice,
// fails the whole call and nothing changes.
func (s *Store) ReorderPlans(orderedRefs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var listed []string
	for _, ref := range orderedRefs {
		p, err := s.planLocked(ref)
		if err != nil {
			return err
		}
		if seen[p.UID] {
			return invalid("plan %q is listed twice", p.ID)
		}
		seen[p.UID] = true
		listed = append(listed, p.UID)
	}
	for _, sm := range s.planSummariesLocked() {
		if !seen[sm.UID] {
			listed = append(listed, sm.UID)
		}
	}
	var changed []Record
	for i, uid := range listed {
		want := i + 1
		if want > MaxOrder {
			return invalid("too many plans to order")
		}
		cur := s.byUID[uid]
		if cur.Order == want {
			continue
		}
		next := cur.clone()
		next.Order = want
		s.touch(&next)
		changed = append(changed, next)
	}
	if len(changed) == 0 {
		return nil
	}
	if err := s.commit(changed...); err != nil {
		return err
	}
	for _, r := range changed {
		s.audit(AuditReordered, r, map[string]any{"order": r.Order}, "")
	}
	return nil
}

// ReorderAutomata sets the order of a plan's Automata. orderedIDs must name
// exactly the Automata linked to the plan, each once.
func (s *Store) ReorderAutomata(planRef string, orderedIDs []string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return Record{}, err
	}
	if len(orderedIDs) != len(plan.Automata) {
		return Record{}, invalid("list every Automaton of the plan exactly once (%d linked, %d given)", len(plan.Automata), len(orderedIDs))
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(orderedIDs))
	for _, raw := range orderedIDs {
		id := strings.TrimSpace(raw)
		if indexOf(plan.Automata, id) < 0 {
			return Record{}, invalid("Automaton %q is not linked to this plan", id)
		}
		if seen[id] {
			return Record{}, invalid("Automaton %q is listed twice", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	same := true
	for i := range ids {
		if ids[i] != plan.Automata[i] {
			same = false
		}
	}
	if same {
		return plan.clone(), nil
	}
	next := plan.clone()
	next.Automata = ids
	s.touch(&next)
	if err := s.commit(next); err != nil {
		return Record{}, err
	}
	s.audit(AuditReordered, next, map[string]any{"automata": ids}, "")
	return next.clone(), nil
}
