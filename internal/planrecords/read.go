package planrecords

import (
	"sort"
	"strings"
)

// Read side of the plans view. Everything here reads the summary index (and the
// link lookups beside it) under one read lock; no full record is read.

// Run classes of a linked Automaton, in run order. Blocked and held items are
// listed after the rest and are skipped by the next marker.
const (
	RunCompleted = "completed"
	RunRunning   = "running"
	RunNext      = "next"
	RunBlocked   = "blocked"
	RunHeld      = "held"
)

func runRank(class string) int {
	switch class {
	case RunCompleted:
		return 0
	case RunRunning:
		return 1
	case RunNext:
		return 2
	case RunBlocked:
		return 3
	}
	return 4
}

// RunClass maps the state last reported for a linked Automaton to its run
// class. An unknown or empty state counts as next.
func RunClass(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "completed", "archived", "done", "shipped":
		return RunCompleted
	case "running", "active", "planning", "in_progress":
		return RunRunning
	case "blocked", "failed":
		return RunBlocked
	case "paused", "rejected", "cancelled", "held", "deferred":
		return RunHeld
	}
	return RunNext
}

// PlanAutomaton is one Automaton of a plan, in run order.
type PlanAutomaton struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	State       string `json:"state,omitempty"`
	Run         string `json:"run"`
	CompletedAt string `json:"completed_at,omitempty"`
	// Position is the place in the plan's own (display) order, from 1.
	Position int `json:"position"`
}

// PlanDigest is a plan with what the plans list shows about it.
type PlanDigest struct {
	Summary       Summary
	AutomationIDs []string
	// Automata are the plan's Automata in run order.
	Automata []PlanAutomaton
	// Done counts the completed Automata.
	Done int
	// Next is the first Automaton in run order that is waiting its turn:
	// blocked and held ones are skipped. Nil when none is.
	Next *PlanAutomaton
}

// runOrder lists a plan's Automata in run order: completed first by completion
// time (an unknown time sorts after a known one, then plan order), then
// running, then next, then blocked, then held; ties keep plan order.
func (s *Store) runOrderLocked(planUID string) []PlanAutomaton {
	ids := s.li.planAutomata[planUID]
	out := make([]PlanAutomaton, 0, len(ids))
	for i, id := range ids {
		info := s.li.linked[linkKey(linkAutomaton, id)].info
		out = append(out, PlanAutomaton{
			ID: id, Title: info.Title, State: info.State, Run: RunClass(info.State),
			CompletedAt: info.CompletedAt, Position: i + 1,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := runRank(a.Run), runRank(b.Run); ra != rb {
			return ra < rb
		}
		if a.Run == RunCompleted && a.CompletedAt != b.CompletedAt {
			if a.CompletedAt == "" || b.CompletedAt == "" {
				return a.CompletedAt != ""
			}
			return a.CompletedAt < b.CompletedAt
		}
		return a.Position < b.Position
	})
	return out
}

func (s *Store) digestLocked(sm Summary) PlanDigest {
	d := PlanDigest{Summary: sm, AutomationIDs: append([]string{}, s.li.planAutomations[sm.UID]...)}
	d.Automata = s.runOrderLocked(sm.UID)
	for i := range d.Automata {
		if d.Automata[i].Run == RunCompleted {
			d.Done++
		}
		if d.Next == nil && d.Automata[i].Run == RunNext {
			d.Next = &d.Automata[i]
		}
	}
	return d
}

// Page is a limit/offset window.
type Page struct {
	Limit  int
	Offset int
}

// PlansView is one page of a project's plans plus its backlog group.
type PlansView struct {
	// Plans are the plans that have at least one Automaton, in priority order.
	Plans      []PlanDigest
	PlansTotal int
	// Backlog are the plans with no Automaton yet, in priority order.
	Backlog      []PlanDigest
	BacklogTotal int
}

// ProjectPlans returns one page of the project's plans and one page of its
// backlog. The Automaton lists of the plans outside the pages are not built.
func (s *Store) ProjectPlans(project string, plans, backlog Page) PlansView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var active, back []Summary
	for _, sm := range s.planSummariesLocked() {
		if project != "" && sm.Project != project {
			continue
		}
		if sm.AutomatonCount == 0 {
			back = append(back, sm)
		} else {
			active = append(active, sm)
		}
	}
	build := func(all []Summary, pg Page) []PlanDigest {
		lo, hi := Filter{Limit: pg.Limit, Offset: pg.Offset}.page(len(all))
		out := make([]PlanDigest, 0, hi-lo)
		for _, sm := range all[lo:hi] {
			out = append(out, s.digestLocked(sm))
		}
		return out
	}
	return PlansView{
		Plans: build(active, plans), PlansTotal: len(active),
		Backlog: build(back, backlog), BacklogTotal: len(back),
	}
}

// PlanAutomataPage returns one page of a plan's Automata in run order, the
// total, the plan, and the next marker over the whole list (not only the page).
func (s *Store) PlanAutomataPage(planRef string, pg Page) (PlanDigest, []PlanAutomaton, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return PlanDigest{}, nil, 0, err
	}
	d := s.digestLocked(s.li.sums[plan.UID])
	lo, hi := Filter{Limit: pg.Limit, Offset: pg.Offset}.page(len(d.Automata))
	return d, append([]PlanAutomaton{}, d.Automata[lo:hi]...), len(d.Automata), nil
}

// PlanAutomatonIDs lists a plan's Automaton ids in run order. It resolves the
// plan from the index only.
func (s *Store) PlanAutomatonIDs(planRef string) (Summary, []PlanAutomaton, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, err := s.planLocked(planRef)
	if err != nil {
		return Summary{}, nil, err
	}
	return s.li.sums[plan.UID], s.runOrderLocked(plan.UID), nil
}

// StateCount is a count per state.
type StateCount map[State]int

// ProjectOverview is the data the overview header and its plan lines read.
type ProjectOverview struct {
	// Active are the plans in flight: not closed, not deferred, and with an
	// Automaton or in progress. Priority order.
	Active []PlanDigest
	// Plans counts plans by state.
	Plans      StateCount
	PlansTotal int
	// Backlog is the number of plans with no Automaton yet.
	Backlog int
	// Rows and Findings count registry rows and findings by state.
	Rows          StateCount
	RowsTotal     int
	Findings      StateCount
	FindingsTotal int
	// Done and Total count the Automata of all plans of the project.
	Done, Total int
}

// Overview reads the project's plans and record counts from the index. The
// Active list holds at most maxActive plans (all of them when maxActive <= 0).
func (s *Store) Overview(project string, maxActive int) ProjectOverview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ov := ProjectOverview{Plans: StateCount{}, Rows: StateCount{}, Findings: StateCount{}}
	for _, sm := range s.planSummariesLocked() {
		if project != "" && sm.Project != project {
			continue
		}
		ov.PlansTotal++
		ov.Plans[sm.State]++
		if sm.AutomatonCount == 0 {
			ov.Backlog++
		}
		ov.Total += sm.AutomatonCount
		d := s.digestLocked(sm)
		ov.Done += d.Done
		live := !IsClosed(KindPlan, sm.State) && sm.State != StateDeferred &&
			(sm.AutomatonCount > 0 || sm.State == StateInProgress)
		if live && (maxActive <= 0 || len(ov.Active) < maxActive) {
			ov.Active = append(ov.Active, d)
		}
	}
	for _, sm := range s.li.sums {
		if project != "" && sm.Project != project {
			continue
		}
		switch sm.Kind {
		case KindRegistryRow:
			ov.RowsTotal++
			ov.Rows[sm.State]++
		case KindFinding:
			ov.FindingsTotal++
			ov.Findings[sm.State]++
		}
	}
	return ov
}

// BrowseFilter narrows Browse. Zero fields match everything. Kind is empty for
// both kinds, else registry_row or finding.
type BrowseFilter struct {
	Query   string
	Kind    Kind
	State   State
	Project string
	Page
}

// BrowseRow is one registry row or finding with the plan and Automata it links to.
type BrowseRow struct {
	Summary  Summary
	PlanID   string
	PlanName string
	Automata []string
}

// Browse lists registry rows and findings from the summary index, filtered by
// title text, kind, state and project, newest-updated first (best title match
// first when a query is given), and the total before paging.
func (s *Store) Browse(f BrowseFilter) ([]BrowseRow, int) {
	toks := strings.Fields(strings.ToLower(f.Query))
	phrase := strings.Join(toks, " ")
	type scored struct {
		sm Summary
		sc int
	}
	s.mu.RLock()
	var hits []scored
	for _, sm := range s.li.sums {
		if sm.Kind != KindRegistryRow && sm.Kind != KindFinding {
			continue
		}
		if (f.Kind != "" && sm.Kind != f.Kind) || (f.State != "" && sm.State != f.State) ||
			(f.Project != "" && sm.Project != f.Project) {
			continue
		}
		sc := titleScore(sm.Title, toks, phrase)
		if sc == 0 {
			continue
		}
		hits = append(hits, scored{sm, sc})
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.sc != b.sc {
			return a.sc > b.sc
		}
		if a.sm.UpdatedAt != b.sm.UpdatedAt {
			return a.sm.UpdatedAt > b.sm.UpdatedAt
		}
		return a.sm.ID < b.sm.ID
	})
	lo, hi := Filter{Limit: f.Limit, Offset: f.Offset}.page(len(hits))
	out := make([]BrowseRow, 0, hi-lo)
	for _, h := range hits[lo:hi] {
		row := BrowseRow{Summary: h.sm, Automata: append([]string{}, h.sm.LinkedAutomata...)}
		var planUID string
		switch {
		case h.sm.Kind == KindFinding && h.sm.AttachKind == AttachPlan:
			if uid, ok := s.byID[h.sm.AttachRef]; ok {
				planUID = uid
			}
		case h.sm.Kind == KindFinding && h.sm.AttachKind == AttachAutomaton:
			row.Automata = appendUnique(row.Automata, h.sm.AttachRef)
			if e, ok := s.li.linked[linkKey(linkAutomaton, h.sm.AttachRef)]; ok {
				planUID = e.planUID
			}
		case h.sm.Kind == KindRegistryRow:
			planUID = s.li.rowPlan[h.sm.ID]
		}
		if planUID != "" {
			if p, ok := s.li.sums[planUID]; ok {
				row.PlanID, row.PlanName = p.ID, p.Title
			}
		}
		out = append(out, row)
	}
	s.mu.RUnlock()
	return out, len(hits)
}

func appendUnique(ids []string, id string) []string {
	for _, v := range ids {
		if v == id {
			return ids
		}
	}
	return append(ids, id)
}
