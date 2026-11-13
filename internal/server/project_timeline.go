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
// Timestamps: an Automaton spans its earliest task StartedAt to its latest
// task CompletedAt across its Story tasks; while it runs the end stays null so
// the client draws it to now. An Automaton with no started task falls back to
// its created time to its completion time. A task starts at StartedAt and ends
// at CompletedAt. An arc spans the earliest start to the latest finish of everything
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
	"fmt"
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

	// Start equals First; End is null while the work runs. Status is one of
	// completed, running, waiting, blocked, failed, cancelled. Release names the
	// release a shipped Automaton merged into. DependsOn lists task ids (task
	// rows only).
	Start     *time.Time `json:"start"`
	End       *time.Time `json:"end"`
	Status    string     `json:"status"`
	Release   string     `json:"release,omitempty"`
	DependsOn []string   `json:"depends_on,omitempty"`
}

// timelineEdge is one dependency between two Automata of an arc: From finishes
// before To starts.
type timelineEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

const timelineEdgeChain = "chain"

// Timeline row statuses.
const (
	tlStatusCompleted = "completed"
	tlStatusRunning   = "running"
	tlStatusWaiting   = "waiting"
	tlStatusBlocked   = "blocked"
	tlStatusFailed    = "failed"
	tlStatusCancelled = "cancelled"
)

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
	// Edges is set on level=automata and focus replies.
	Edges []timelineEdge `json:"edges,omitempty"`
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

// timelineTaskStatus maps a task status to a timeline status.
func timelineTaskStatus(st autonomous.TaskStatus) string {
	switch st {
	case autonomous.TaskCompleted:
		return tlStatusCompleted
	case autonomous.TaskInProgress, autonomous.TaskRunningTests, autonomous.TaskVerifying:
		return tlStatusRunning
	case autonomous.TaskFailed:
		return tlStatusFailed
	case autonomous.TaskBlocked, autonomous.TaskReviewNeeded:
		return tlStatusBlocked
	case autonomous.TaskCancelled:
		return tlStatusCancelled
	}
	return tlStatusWaiting
}

// timelineAutomatonStatus maps an Automaton to a timeline status. A shipped
// Automaton reads as completed no matter its recorded status, including a
// cancelled or rejected one; an unshipped Automaton maps its status as-is.
func timelineAutomatonStatus(p *autonomous.PRD) string {
	if p.IsShipped() || p.MergedInRelease != "" {
		return tlStatusCompleted
	}
	switch p.Status {
	case autonomous.PRDRunning, autonomous.PRDActive:
		return tlStatusRunning
	case autonomous.PRDCompleted, autonomous.PRDArchived:
		return tlStatusCompleted
	case autonomous.PRDFailed:
		return tlStatusFailed
	case autonomous.PRDBlocked:
		return tlStatusBlocked
	case autonomous.PRDCancelled, autonomous.PRDRejected:
		return tlStatusCancelled
	}
	return tlStatusWaiting
}

// timelineTaskRow is the row of one task: it spans StartedAt to CompletedAt.
func timelineTaskRow(t *autonomous.Task) timelineRow {
	running := timelineTaskRunning(t)
	last := t.CompletedAt
	if running {
		last = nil
	}
	return timelineRow{Kind: timelineKindTask, ID: t.ID, Title: t.Title, State: string(t.Status),
		First: t.StartedAt, Last: last, Running: running,
		Start: t.StartedAt, End: last, Status: timelineTaskStatus(t.Status),
		DependsOn: append([]string(nil), t.DependsOn...)}
}

// timelineAutomatonSpan is the span of an Automaton: its earliest task start
// to its latest task completion across all of its Story tasks, or now (end
// null) while it runs. An Automaton with no started task falls back to its
// created time to its completion time so the row still draws. The span is
// never shorter in a way that hides real work: it never ends before it starts.
func timelineAutomatonSpan(p *autonomous.PRD) timelineSpan {
	running := timelineAutomatonRunning(p)
	var s timelineSpan
	anyStarted := false
	for _, st := range p.Story {
		for i := range st.Tasks {
			t := &st.Tasks[i]
			s.add(t.StartedAt, t.CompletedAt, timelineTaskRunning(t))
			if t.StartedAt != nil {
				anyStarted = true
			}
		}
	}
	if anyStarted {
		if s.first != nil && s.last != nil && s.last.Before(*s.first) {
			l := *s.first
			s.last = &l
		}
		return s
	}
	var end *time.Time
	switch timelineAutomatonStatus(p) {
	case tlStatusCompleted, tlStatusFailed, tlStatusCancelled:
		end = timePtr(p.UpdatedAt)
	}
	s.add(timePtr(p.CreatedAt), end, running)
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
	sp := timelineAutomatonSpan(p)
	first, last := sp.done()
	n := timelineTaskCount(p)
	return timelineRow{Kind: timelineKindAutomaton, ID: p.ID, Title: p.Title, State: string(p.Status),
		First: first, Last: last, Running: sp.running,
		HasChildren: n > 0, ChildrenCount: n,
		Start: first, End: last, Status: timelineAutomatonStatus(p), Release: p.MergedInRelease}
}

// timelineArcStatus derives an arc or group status from its members.
func timelineArcStatus(state string, members []*autonomous.PRD) string {
	if len(members) == 0 {
		if state == config.ArcStateDone {
			return tlStatusCompleted
		}
		return tlStatusWaiting
	}
	var run, fail, block, done, cancel int
	for _, p := range members {
		switch timelineAutomatonStatus(p) {
		case tlStatusRunning:
			run++
		case tlStatusFailed:
			fail++
		case tlStatusBlocked:
			block++
		case tlStatusCompleted:
			done++
		case tlStatusCancelled:
			cancel++
		}
	}
	switch {
	case run > 0:
		return tlStatusRunning
	case fail > 0:
		return tlStatusFailed
	case block > 0:
		return tlStatusBlocked
	case done+cancel == len(members) && done > 0, state == config.ArcStateDone:
		return tlStatusCompleted
	case cancel == len(members):
		return tlStatusCancelled
	}
	return tlStatusWaiting
}

// timelineRowKey is the stable cursor key of a row.
func timelineRowKey(r timelineRow) string { return r.Kind + ":" + r.ID }

// timelineBaseLess is the order inside an arc before dependencies apply:
// finished rows first by end time, then the rest by start, then id.
func timelineBaseLess(a, b timelineRow) bool {
	af, bf := a.End != nil, b.End != nil
	if af != bf {
		return af
	}
	if af {
		if !a.End.Equal(*b.End) {
			return a.End.Before(*b.End)
		}
	} else if (a.Start == nil) != (b.Start == nil) {
		return a.Start != nil
	} else if a.Start != nil && !a.Start.Equal(*b.Start) {
		return a.Start.Before(*b.Start)
	}
	return a.ID < b.ID
}

// orderTimelineRows orders rows of one kind: completion order (see
// timelineBaseLess), then a topological pass so a dependency sits above its
// dependent, ties broken by the completion order. Dependencies are the rows'
// DependsOn plus the edges; ids not among the rows are ignored. On a cycle the
// completion order is returned. The input is not changed.
func orderTimelineRows(rows []timelineRow, edges []timelineEdge) []timelineRow {
	base := append([]timelineRow(nil), rows...)
	sort.SliceStable(base, func(i, j int) bool { return timelineBaseLess(base[i], base[j]) })
	idx := make(map[string]int, len(base))
	for i, r := range base {
		idx[r.ID] = i
	}
	indeg := make([]int, len(base))
	next := make([][]int, len(base))
	seen := map[[2]int]bool{}
	link := func(from, to string) {
		f, ok1 := idx[from]
		t, ok2 := idx[to]
		if !ok1 || !ok2 || f == t || seen[[2]int{f, t}] {
			return
		}
		seen[[2]int{f, t}] = true
		next[f] = append(next[f], t)
		indeg[t]++
	}
	for _, r := range base {
		for _, d := range r.DependsOn {
			link(d, r.ID)
		}
	}
	for _, e := range edges {
		link(e.From, e.To)
	}
	out := make([]timelineRow, 0, len(base))
	done := make([]bool, len(base))
	for len(out) < len(base) {
		pick := -1
		for i := range base {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick < 0 {
			return base // cycle
		}
		done[pick] = true
		out = append(out, base[pick])
		for _, t := range next[pick] {
			indeg[t]--
		}
	}
	return out
}

// timelineAutomationEdges builds the chain edges between Automata of one
// Automation: outcome edges, fork branches, a branch's paired join, and the
// member order for a member with no other way on. Members that are not
// Automata (fork, join, nested, pending) are passed through.
func timelineAutomationEdges(a *autonomous.Automation) []timelineEdge {
	if a == nil {
		return nil
	}
	key := func(i int) string {
		if a.Members[i].ID != "" {
			return a.Members[i].ID
		}
		return fmt.Sprintf("#%d", i)
	}
	pairedJoin := map[string]string{}
	for i, m := range a.Members {
		if m.Kind == autonomous.MemberKindJoin {
			for _, b := range m.JoinMembers {
				pairedJoin[b] = key(i)
			}
		}
	}
	succ := map[string][]string{}
	prd := map[string]string{}
	var order []string
	for i, m := range a.Members {
		k := key(i)
		order = append(order, k)
		if (m.Kind == "" || m.Kind == autonomous.MemberKindPRD) && m.PRDID != "" {
			prd[k] = m.PRDID
		}
		var n []string
		for _, t := range []string{m.OnComplete, m.OnBlocked, m.OnFailed} {
			if t != "" {
				n = append(n, t)
			}
		}
		if m.Kind == autonomous.MemberKindFork {
			n = append(n, m.ForkMembers...)
		}
		if j, ok := pairedJoin[k]; ok {
			n = append(n, j)
		}
		if len(n) == 0 && i+1 < len(a.Members) {
			n = append(n, key(i+1))
		}
		succ[k] = n
	}
	var leaves func(k string, seen map[string]bool) []string
	leaves = func(k string, seen map[string]bool) []string {
		if id, ok := prd[k]; ok {
			return []string{id}
		}
		if seen[k] {
			return nil
		}
		seen[k] = true
		var out []string
		for _, n := range succ[k] {
			out = append(out, leaves(n, seen)...)
		}
		return out
	}
	var edges []timelineEdge
	dup := map[[2]string]bool{}
	for _, k := range order {
		from, ok := prd[k]
		if !ok {
			continue
		}
		for _, n := range succ[k] {
			for _, to := range leaves(n, map[string]bool{}) {
				if to == from || dup[[2]string{from, to}] {
					continue
				}
				dup[[2]string{from, to}] = true
				edges = append(edges, timelineEdge{From: from, To: to, Kind: timelineEdgeChain})
			}
		}
	}
	return edges
}

// timelineEdgesAmong keeps the edges of the given Automations whose two ends
// are both in ids.
func timelineEdgesAmong(autos []*autonomous.Automation, ids map[string]bool) []timelineEdge {
	edges := []timelineEdge{}
	dup := map[[2]string]bool{}
	for _, a := range autos {
		for _, e := range timelineAutomationEdges(a) {
			if ids[e.From] && ids[e.To] && !dup[[2]string{e.From, e.To}] {
				dup[[2]string{e.From, e.To}] = true
				edges = append(edges, e)
			}
		}
	}
	return edges
}

// sortAutomataByStart orders Automata by created time (unknown last), then id.
func sortAutomataByStart(ps []*autonomous.PRD) {
	sort.SliceStable(ps, func(i, j int) bool {
		si, sj := ps[i].CreatedAt, ps[j].CreatedAt
		if si.IsZero() != sj.IsZero() {
			return !si.IsZero()
		}
		if !si.Equal(sj) {
			return si.Before(sj)
		}
		return ps[i].ID < ps[j].ID
	})
}

// timelineOrderedTasks returns a PRD's task rows ordered by depends_on.
func timelineOrderedTasks(p *autonomous.PRD) []timelineRow {
	var rows []timelineRow
	for _, st := range p.Story {
		for i := range st.Tasks {
			rows = append(rows, timelineTaskRow(&st.Tasks[i]))
		}
	}
	return orderTimelineRows(rows, nil)
}

// timelineOrderedAutomata returns the rows of list ordered inside the arc, and
// the chain edges among them drawn from the given Automations.
func timelineOrderedAutomata(list []*autonomous.PRD, autos []*autonomous.Automation) ([]timelineRow, []timelineEdge) {
	rows := make([]timelineRow, 0, len(list))
	ids := map[string]bool{}
	for _, p := range list {
		rows = append(rows, timelineAutomatonRow(p))
		ids[p.ID] = true
	}
	edges := timelineEdgesAmong(autos, ids)
	return orderTimelineRows(rows, edges), edges
}

// timelineAutomationsFor picks the Automations whose edges apply to a parent:
// one arc, none for the unassigned group, or every arc when parent is empty.
func timelineAutomationsFor(parent string, arcs map[string]bool, autos []*autonomous.Automation) []*autonomous.Automation {
	var out []*autonomous.Automation
	for _, a := range autos {
		if a == nil || !arcs[a.ID] {
			continue
		}
		if parent == "" || parent == a.ID {
			out = append(out, a)
		}
	}
	return out
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
		byID := map[string]*autonomous.PRD{}
		for _, p := range list {
			byID[p.ID] = p
		}
		autoRows, edges := timelineOrderedAutomata(list, timelineAutomationsFor(in.Focus, mem.arcs, in.Automations))
		out.Edges = edges
		for _, ar := range autoRows {
			rows = append(rows, ar)
			rows = append(rows, timelineOrderedTasks(byID[ar.ID])...)
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
				First: first, Last: last, Running: s.running, HasChildren: n > 0 && first != nil, ChildrenCount: n,
				Start: first, End: last, Status: timelineArcStatus(arc.State, byArc[arc.AutomationID])})
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
				HasChildren: first != nil, ChildrenCount: len(unassigned),
				Start: first, End: last, Status: timelineArcStatus("", unassigned)})
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
		rows, out.Edges = timelineOrderedAutomata(list, timelineAutomationsFor(in.Parent, mem.arcs, in.Automations))
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
		rows = timelineOrderedTasks(found)
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
