// Project timeline read: GET /api/projects/{name}/timeline.
//
// The timeline answers "when did each arc, Automaton and task of this project
// start and finish" without fetching every task up front. It returns one level
// at a time: arcs (default), the Automata under an arc, or the tasks of one
// Automaton. A client draws the arc bars first and asks for children only for
// the rows it expands. focus=<arc> returns one arc's Automata and their tasks in
// a single reply.
//
// Membership follows the project roll-up: an Automaton belongs to the arc whose
// Automation holds it (PRD.AutomationID, else the Automation whose members
// include it) when that arc belongs to the project. Only an Automaton with no
// matching arc and a ProjectDir equal to the project's directory goes to the
// "unassigned" group. Templates are left out.
//
// Timestamps: an Automaton starts at ApprovedAt, else CreatedAt, and finishes at
// UpdatedAt (see historyAutomatonTimes). A task starts at StartedAt and ends at
// CompletedAt. An arc spans the earliest start to the latest finish of everything
// under it. Work that is still running (an Automaton in a running status, or a
// task with a start and no completion that is still in flight) has last=null and
// running=true so the client draws it to now. An arc or Automaton with nothing
// to draw has first=null and last=null.
//
// Query: level=arcs|automata|tasks, parent=<arc id>|none (level=automata),
// automaton=<Automaton id> (level=tasks), focus=<arc id>|none, limit (1-200,
// default 50) and cursor (opaque) to page the rows of a level. A focus reply is
// not paged. Reads require CapAutonomousRead, as the other project reads do.

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	timelineLevelArcs     = "arcs"
	timelineLevelAutomata = "automata"
	timelineLevelTasks    = "tasks"
	timelineLevelFocus    = "focus"

	timelineKindArc        = "arc"
	timelineKindAutomaton  = "automaton"
	timelineKindTask       = "task"
	timelineKindUnassigned = "unassigned"

	// timelineNone is the parent value for Automata that no arc holds.
	timelineNone = "none"
)

// timelineRow is one row of the reply.
type timelineRow struct {
	Kind          string     `json:"kind"`
	ID            string     `json:"id"`
	Title         string     `json:"title,omitempty"`
	State         string     `json:"state"`
	First         *time.Time `json:"first"`
	Last          *time.Time `json:"last"`
	Running       bool       `json:"running"`
	HasChildren   bool       `json:"has_children"`
	ChildrenCount int        `json:"children_count"`
}

// projectTimeline is the GET /api/projects/{name}/timeline response.
type projectTimeline struct {
	Project     string        `json:"project"`
	GeneratedAt time.Time     `json:"generated_at"`
	Level       string        `json:"level"`
	Parent      string        `json:"parent"`
	Limit       int           `json:"limit"`
	Total       int           `json:"total"`
	HasMore     bool          `json:"has_more"`
	NextCursor  string        `json:"next_cursor,omitempty"`
	Rows        []timelineRow `json:"rows"`
}

// timelineInput is everything buildProjectTimeline reads. It holds plain
// records so the levels and paging can be tested without a running daemon.
type timelineInput struct {
	Project     string
	Dir         string
	Arcs        []config.ProjectArc
	PRDs        []*autonomous.PRD
	Automations []*autonomous.Automation
	Level       string // arcs (default), automata or tasks
	Parent      string // level=automata: an arc's Automation id, or "none"; empty means all
	Automaton   string // level=tasks: the Automaton id
	Focus       string // an arc's Automation id (or "none"); overrides Level
	Limit       int
	Cursor      *historyCursor
	Now         time.Time
}

// timelineError says which client mistake or missing record stopped a build.
type timelineError struct {
	Status int
	Msg    string
}

func (e *timelineError) Error() string { return e.Msg }

// projectMembership places an Automaton in one project: in the arc that holds
// it, in the unassigned group, or nowhere. It is the one place the explicit
// link is tried before the directory.
type projectMembership struct {
	memberOf map[string]string // PRD id -> Automation id holding it as a member
	arcs     map[string]bool   // Automation ids that are arcs of the project
	cleanDir string
}

func newProjectMembership(arcs []config.ProjectArc, automations []*autonomous.Automation, dir string) projectMembership {
	m := projectMembership{memberOf: map[string]string{}, arcs: map[string]bool{}, cleanDir: filepath.Clean(dir)}
	for _, a := range automations {
		if a == nil {
			continue
		}
		for _, mem := range a.Members {
			if mem.PRDID == "" {
				continue
			}
			if _, ok := m.memberOf[mem.PRDID]; !ok {
				m.memberOf[mem.PRDID] = a.ID
			}
		}
	}
	for _, arc := range arcs {
		m.arcs[arc.AutomationID] = true
	}
	return m
}

// place returns the arc id that holds p, or unassigned=true when only the
// directory matches. ok is false for a template or an Automaton of another project.
func (m projectMembership) place(p *autonomous.PRD) (arc string, unassigned, ok bool) {
	if p == nil || p.IsTemplate {
		return "", false, false
	}
	auto := p.AutomationID
	if auto == "" {
		auto = m.memberOf[p.ID]
	}
	if auto != "" && m.arcs[auto] {
		return auto, false, true
	}
	if p.ProjectDir != "" && filepath.Clean(p.ProjectDir) == m.cleanDir {
		return "", true, true
	}
	return "", false, false
}

// span accumulates the first and last time of a group of work.
type timelineSpan struct {
	first, last *time.Time
	running     bool
}

func (s *timelineSpan) add(first, last *time.Time, running bool) {
	if first != nil && (s.first == nil || first.Before(*s.first)) {
		f := *first
		s.first = &f
	}
	if last != nil && (s.last == nil || last.After(*s.last)) {
		l := *last
		s.last = &l
	}
	if running {
		s.running = true
	}
}

// done returns the span as reported: running work has no last.
func (s timelineSpan) done() (first, last *time.Time) {
	last = s.last
	if s.running {
		last = nil
	}
	return s.first, last
}

func timelineAutomatonRunning(p *autonomous.PRD) bool {
	return p.Status == autonomous.PRDRunning || p.Status == autonomous.PRDActive
}

func timelineTaskRunning(t *autonomous.Task) bool {
	if t.StartedAt == nil || t.CompletedAt != nil {
		return false
	}
	return t.Status == autonomous.TaskInProgress || t.Status == autonomous.TaskVerifying
}

// timelineTaskRow is the row of one task.
func timelineTaskRow(t *autonomous.Task) timelineRow {
	running := timelineTaskRunning(t)
	last := t.CompletedAt
	if running {
		last = nil
	}
	return timelineRow{Kind: timelineKindTask, ID: t.ID, Title: t.Title, State: string(t.Status),
		First: t.StartedAt, Last: last, Running: running}
}

// timelineAutomatonSpan is the span of an Automaton and everything in it.
func timelineAutomatonSpan(p *autonomous.PRD) timelineSpan {
	var s timelineSpan
	start, finish := historyAutomatonTimes(p)
	s.add(timePtr(start), timePtr(finish), timelineAutomatonRunning(p))
	for _, st := range p.Story {
		for i := range st.Tasks {
			t := &st.Tasks[i]
			s.add(t.StartedAt, t.CompletedAt, timelineTaskRunning(t))
		}
	}
	return s
}

func timelineTaskCount(p *autonomous.PRD) int {
	n := 0
	for _, st := range p.Story {
		n += len(st.Tasks)
	}
	return n
}

func timelineAutomatonRow(p *autonomous.PRD) timelineRow {
	first, last := timelineAutomatonSpan(p).done()
	n := timelineTaskCount(p)
	return timelineRow{Kind: timelineKindAutomaton, ID: p.ID, Title: p.Title, State: string(p.Status),
		First: first, Last: last, Running: timelineAutomatonSpan(p).running,
		HasChildren: n > 0, ChildrenCount: n}
}

// timelineRowKey is the stable cursor key of a row.
func timelineRowKey(r timelineRow) string { return r.Kind + ":" + r.ID }

// sortAutomataByStart orders Automata by first time (unknown last), then id.
func sortAutomataByStart(ps []*autonomous.PRD) {
	sort.SliceStable(ps, func(i, j int) bool {
		si, _ := historyAutomatonTimes(ps[i])
		sj, _ := historyAutomatonTimes(ps[j])
		if si.IsZero() != sj.IsZero() {
			return !si.IsZero()
		}
		if !si.Equal(sj) {
			return si.Before(sj)
		}
		return ps[i].ID < ps[j].ID
	})
}

// buildProjectTimeline builds one level of the timeline.
func buildProjectTimeline(in timelineInput) (projectTimeline, *timelineError) {
	limit := in.Limit
	if limit < 1 {
		limit = historyDefaultLimit
	}
	level := in.Level
	if level == "" {
		level = timelineLevelArcs
	}
	out := projectTimeline{Project: in.Project, GeneratedAt: in.Now, Level: level, Limit: limit, Rows: []timelineRow{}}

	mem := newProjectMembership(in.Arcs, in.Automations, in.Dir)
	byArc := map[string][]*autonomous.PRD{}
	var unassigned, all []*autonomous.PRD
	for _, p := range in.PRDs {
		arc, un, ok := mem.place(p)
		if !ok {
			continue
		}
		all = append(all, p)
		if un {
			unassigned = append(unassigned, p)
		} else {
			byArc[arc] = append(byArc[arc], p)
		}
	}
	for _, list := range byArc {
		sortAutomataByStart(list)
	}
	sortAutomataByStart(unassigned)
	sortAutomataByStart(all)

	var rows []timelineRow
	paged := true
	switch {
	case in.Focus != "":
		out.Level = timelineLevelFocus
		out.Parent = in.Focus
		var list []*autonomous.PRD
		if in.Focus == timelineNone {
			list = unassigned
		} else if mem.arcs[in.Focus] {
			list = byArc[in.Focus]
		} else {
			return out, &timelineError{http.StatusNotFound, "arc not found"}
		}
		for _, p := range list {
			rows = append(rows, timelineAutomatonRow(p))
			for _, st := range p.Story {
				for i := range st.Tasks {
					rows = append(rows, timelineTaskRow(&st.Tasks[i]))
				}
			}
		}
		paged = false
	case level == timelineLevelArcs:
		seen := map[string]bool{}
		names := map[string]string{}
		for _, a := range in.Automations {
			if a != nil {
				names[a.ID] = a.Name
			}
		}
		for _, arc := range in.Arcs {
			if seen[arc.AutomationID] {
				continue
			}
			seen[arc.AutomationID] = true
			state := arc.State
			if state == "" {
				state = config.ArcStatePlanned
			}
			title := names[arc.AutomationID]
			if title == "" {
				title = arc.AutomationID
			}
			var s timelineSpan
			for _, p := range byArc[arc.AutomationID] {
				sp := timelineAutomatonSpan(p)
				s.add(sp.first, sp.last, sp.running)
			}
			first, last := s.done()
			n := len(byArc[arc.AutomationID])
			rows = append(rows, timelineRow{Kind: timelineKindArc, ID: arc.AutomationID, Title: title, State: state,
				First: first, Last: last, Running: s.running, HasChildren: n > 0 && first != nil, ChildrenCount: n})
		}
		if len(unassigned) > 0 {
			var s timelineSpan
			for _, p := range unassigned {
				sp := timelineAutomatonSpan(p)
				s.add(sp.first, sp.last, sp.running)
			}
			first, last := s.done()
			rows = append(rows, timelineRow{Kind: timelineKindUnassigned, ID: timelineNone, Title: "Unassigned",
				State: config.ArcStatePlanned, First: first, Last: last, Running: s.running,
				HasChildren: first != nil, ChildrenCount: len(unassigned)})
		}
	case level == timelineLevelAutomata:
		out.Parent = in.Parent
		list := all
		switch {
		case in.Parent == "":
		case in.Parent == timelineNone:
			list = unassigned
		case mem.arcs[in.Parent]:
			list = byArc[in.Parent]
		default:
			return out, &timelineError{http.StatusNotFound, "arc not found"}
		}
		for _, p := range list {
			rows = append(rows, timelineAutomatonRow(p))
		}
	case level == timelineLevelTasks:
		out.Parent = in.Automaton
		if in.Automaton == "" {
			return out, &timelineError{http.StatusBadRequest, "automaton is required for level=tasks"}
		}
		var found *autonomous.PRD
		for _, p := range all {
			if p.ID == in.Automaton {
				found = p
				break
			}
		}
		if found == nil {
			return out, &timelineError{http.StatusNotFound, "automaton not found"}
		}
		for _, st := range found.Story {
			for i := range st.Tasks {
				rows = append(rows, timelineTaskRow(&st.Tasks[i]))
			}
		}
	default:
		return out, &timelineError{http.StatusBadRequest, "level must be arcs, automata or tasks"}
	}

	out.Total = len(rows)
	if !paged {
		out.Rows = append(out.Rows, rows...)
		return out, nil
	}
	start := 0
	if in.Cursor != nil {
		start = -1
		for i, r := range rows {
			if timelineRowKey(r) == in.Cursor.K {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return out, &timelineError{http.StatusBadRequest, "invalid cursor"}
		}
	}
	end := start + limit
	if end >= len(rows) {
		end = len(rows)
	} else {
		out.HasMore = true
	}
	out.Rows = append(out.Rows, rows[start:end]...)
	if out.HasMore {
		out.NextCursor = encodeHistoryCursor(historyEntry{key: timelineRowKey(out.Rows[len(out.Rows)-1])})
	}
	return out, nil
}

// handleProjectTimeline serves GET /api/projects/{name}/timeline.
func (s *Server) handleProjectTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	level := q.Get("level")
	switch level {
	case "", timelineLevelArcs, timelineLevelAutomata, timelineLevelTasks:
	default:
		http.Error(w, "level must be arcs, automata or tasks", http.StatusBadRequest)
		return
	}
	limit, err := parseHistoryLimit(q.Get("limit"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cursor, err := decodeHistoryCursor(q.Get("cursor"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		http.Error(w, "automations not available", http.StatusServiceUnavailable)
		return
	}
	resp, terr := buildProjectTimeline(timelineInput{
		Project:     name,
		Dir:         entry.Dir,
		Arcs:        entry.Arcs,
		PRDs:        mgr.Store().ListPRDs(),
		Automations: mgr.ListAutomations(),
		Level:       level,
		Parent:      q.Get("parent"),
		Automaton:   q.Get("automaton"),
		Focus:       q.Get("focus"),
		Limit:       limit,
		Cursor:      cursor,
		Now:         time.Now(),
	})
	if terr != nil {
		http.Error(w, terr.Msg, terr.Status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
