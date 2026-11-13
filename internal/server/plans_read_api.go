// plans_read_api.go — the plans read API.
//
//	GET /api/projects/{name}/plans            plans in order, Backlog group (autonomous:read)
//	GET /api/projects/{name}/overview-summary header, progress, active plan lines, counts
//	GET /api/plans/rollup?project=&dry_run=true what the roll-up would change (plans_rollup_api.go)
//	GET /api/plans/{ref}/automata             a plan's Automata in run order
//	GET /api/plans/{ref}/tasks                a plan's tasks, read when a plan is expanded
//	GET /api/plan-records/browse              registry rows and findings, searchable
//	GET /api/projects/{name}/arcs             deprecated: the roll-up arcs plus plans
//
// Every read here answers from the plan-record summary index, never from a
// full record. The one exception is the tasks route: a task is not in the
// index, so it reads the Automata of the one plan that was expanded.
//
// Time budget: each of the index reads must answer in about a second for a
// project with thousands of plans, rows and findings; plans_read_api_test.go
// holds that budget against a large fixture. Lists are paged (limit/offset,
// default 50, at most 500) so a response stays small.
//
// Points are the cost report's usage points for each Automaton (never API
// dollars). They come from a short-lived cache, so a request never runs the
// cost report itself. An Automaton with no figure reads as 0.
//
// autonomous.plan_records.enabled false answers every route with 404. A
// request only reads; it never starts, signals or restarts a process.

package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// planPointsTTL is how long the per-Automaton points are reused.
const planPointsTTL = time.Minute

// planProgress is done out of total, with a whole-number percent for a bar.
type planProgress struct {
	Done    int `json:"done"`
	Total   int `json:"total"`
	Percent int `json:"percent"`
}

func newPlanProgress(done, total int) planProgress {
	p := planProgress{Done: done, Total: total}
	if total > 0 {
		p.Percent = done * 100 / total
	}
	return p
}

// planNextMarker names the Automaton that runs next in a plan.
type planNextMarker struct {
	AutomatonID string `json:"automaton_id"`
	Title       string `json:"title,omitempty"`
}

// planRow is one plan of the plans list.
type planRow struct {
	Title         string          `json:"title"`
	ID            string          `json:"id"`
	UID           string          `json:"uid"`
	Goal          string          `json:"goal,omitempty"`
	State         string          `json:"state"`
	SetByHand     bool            `json:"set_by_hand"`
	Order         int             `json:"order,omitempty"`
	Progress      planProgress    `json:"progress"`
	Points        float64         `json:"points"`
	AutomationIDs []string        `json:"automation_ids"`
	AutomatonIDs  []string        `json:"automaton_ids"`
	FindingCount  int             `json:"finding_count"`
	Next          *planNextMarker `json:"next,omitempty"`
}

func (s *Server) planRowFor(d planrecords.PlanDigest, points map[string]float64) planRow {
	row := planRow{
		Title: d.Summary.Title, ID: d.Summary.ID, UID: d.Summary.UID, Goal: d.Summary.Goal,
		State: string(d.Summary.State), SetByHand: d.Summary.SetByHand, Order: d.Summary.Order,
		Progress:      newPlanProgress(d.Done, len(d.Automata)),
		AutomationIDs: d.AutomationIDs, AutomatonIDs: make([]string, 0, len(d.Automata)),
		FindingCount: d.Summary.FindingCount,
	}
	if row.AutomationIDs == nil {
		row.AutomationIDs = []string{}
	}
	for _, a := range d.Automata {
		row.AutomatonIDs = append(row.AutomatonIDs, a.ID)
		row.Points += points[a.ID]
	}
	if d.Next != nil {
		row.Next = &planNextMarker{AutomatonID: d.Next.ID, Title: d.Next.Title}
	}
	return row
}

// planPointsCache holds the last per-Automaton points and when they were read.
type planPointsCache struct {
	mu   sync.Mutex
	at   time.Time
	data map[string]float64
}

// automatonPoints returns points per Automaton id. A test sets planPointsSource;
// otherwise the cost report prices the Automata once a minute at most.
func (s *Server) automatonPoints() map[string]float64 {
	if s.planPointsSource != nil {
		return s.planPointsSource()
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		return nil
	}
	c := &s.planPoints
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && time.Since(c.at) < planPointsTTL {
		return c.data
	}
	now := time.Now()
	c.data = s.costRollupPoints(mgr, now, rollupMaxWindowDays)
	c.at = now
	return c.data
}

// planReadGate runs the checks every route shares: plan records on, the read
// capability, and the store present. It answers the failure and returns false.
func (s *Server) planReadGate(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		planRecordsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return false
	}
	if !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		http.Error(w, "plan records are turned off (autonomous.plan_records.enabled)", http.StatusNotFound)
		return false
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return false
	}
	if s.planRecords == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "plan records are not available")
		return false
	}
	return true
}

// pageParams reads limit and offset. A prefix names a second pair
// ("backlog_limit"). Missing limit is the default; an invalid value answers 400.
func pageParams(w http.ResponseWriter, r *http.Request, prefix string) (planrecords.Page, bool) {
	var pg planrecords.Page
	q := r.URL.Query()
	for name, dst := range map[string]*int{prefix + "limit": &pg.Limit, prefix + "offset": &pg.Offset} {
		v := q.Get(name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || (strings.HasSuffix(name, "limit") && n == 0) {
			planRecordsError(w, http.StatusBadRequest, name+" must be a positive whole number")
			return pg, false
		}
		*dst = n
	}
	return pg, true
}

func pageMeta(pg planrecords.Page, total int) map[string]int {
	lim := pg.Limit
	if lim <= 0 {
		lim = planrecords.DefaultLimit
	}
	if lim > planrecords.MaxLimit {
		lim = planrecords.MaxLimit
	}
	return map[string]int{"total": total, "limit": lim, "offset": pg.Offset}
}

// planProject checks the {name} project exists.
func (s *Server) planProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if _, ok := s.cfg.Projects[name]; !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return "", false
	}
	return name, true
}

// handleProjectPlans serves GET /api/projects/{name}/plans.
func (s *Server) handleProjectPlans(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	name, ok := s.planProject(w, r)
	if !ok {
		return
	}
	pg, ok := pageParams(w, r, "")
	if !ok {
		return
	}
	bpg, ok := pageParams(w, r, "backlog_")
	if !ok {
		return
	}
	if bpg.Limit == 0 {
		bpg.Limit = pg.Limit
	}
	view := s.planRecords.ProjectPlans(name, pg, bpg)
	points := s.automatonPoints()
	rows := make([]planRow, 0, len(view.Plans))
	for _, d := range view.Plans {
		rows = append(rows, s.planRowFor(d, points))
	}
	back := make([]planRow, 0, len(view.Backlog))
	for _, d := range view.Backlog {
		back = append(back, s.planRowFor(d, points))
	}
	writeJSONOK(w, map[string]any{
		"project": name,
		"plans":   rows,
		"page":    pageMeta(pg, view.PlansTotal),
		"backlog": map[string]any{"plans": back, "page": pageMeta(bpg, view.BacklogTotal)},
	})
}

// overviewPlanLine is one active plan of the overview: state and a progress bar.
type overviewPlanLine struct {
	Title     string       `json:"title"`
	ID        string       `json:"id"`
	State     string       `json:"state"`
	SetByHand bool         `json:"set_by_hand"`
	Progress  planProgress `json:"progress"`
}

// handleProjectOverviewSummary serves GET /api/projects/{name}/overview-summary.
func (s *Server) handleProjectOverviewSummary(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	name, ok := s.planProject(w, r)
	if !ok {
		return
	}
	ov := s.planRecords.Overview(name, planrecords.DefaultLimit)
	points := s.automatonPoints()
	lines := make([]overviewPlanLine, 0, len(ov.Active))
	var next map[string]any
	var pts float64
	for _, d := range ov.Active {
		lines = append(lines, overviewPlanLine{
			Title: d.Summary.Title, ID: d.Summary.ID, State: string(d.Summary.State), SetByHand: d.Summary.SetByHand,
			Progress: newPlanProgress(d.Done, len(d.Automata)),
		})
		for _, a := range d.Automata {
			pts += points[a.ID]
		}
		if next == nil && d.Next != nil {
			next = map[string]any{
				"plan_id": d.Summary.ID, "plan_title": d.Summary.Title,
				"automaton_id": d.Next.ID, "automaton_title": d.Next.Title,
			}
		}
	}
	entry := s.cfg.Projects[name]
	writeJSONOK(w, map[string]any{
		"header": map[string]any{
			"project": name, "dir": entry.Dir, "generated_at": time.Now().UTC().Format(time.RFC3339),
		},
		"progress": map[string]any{
			"automata": newPlanProgress(ov.Done, ov.Total), "points": pts,
		},
		"active_plans": lines,
		"next":         next,
		"counts": map[string]any{
			"plans":    map[string]any{"total": ov.PlansTotal, "backlog": ov.Backlog, "by_state": ov.Plans},
			"records":  map[string]any{"total": ov.RowsTotal + ov.FindingsTotal},
			"rows":     map[string]any{"total": ov.RowsTotal, "by_state": ov.Rows},
			"findings": map[string]any{"total": ov.FindingsTotal, "by_state": ov.Findings},
		},
	})
}

// planAutomatonRow is one Automaton of a plan in run order.
type planAutomatonRow struct {
	Title       string  `json:"title"`
	ID          string  `json:"id"`
	State       string  `json:"state,omitempty"`
	Run         string  `json:"run"`
	Position    int     `json:"position"`
	CompletedAt string  `json:"completed_at,omitempty"`
	Points      float64 `json:"points"`
	// Skipped is true for a blocked or held Automaton: shown, but never the next marker.
	Skipped bool `json:"skipped,omitempty"`
	IsNext  bool `json:"is_next,omitempty"`
}

// planRefOf resolves {ref}, answering the failure itself.
func (s *Server) planRefOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	ref := normalizePlanRef(r.PathValue("ref"))
	if ref == "" {
		planRecordsError(w, http.StatusBadRequest, "plan reference is required")
		return "", false
	}
	return ref, true
}

// handlePlanAutomata serves GET /api/plans/{ref}/automata.
func (s *Server) handlePlanAutomata(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	pg, ok := pageParams(w, r, "")
	if !ok {
		return
	}
	d, page, total, err := s.planRecords.PlanAutomataPage(ref, pg)
	if err != nil {
		writePlanRecordStoreError(w, err, "read the plan")
		return
	}
	points := s.automatonPoints()
	rows := make([]planAutomatonRow, 0, len(page))
	for _, a := range page {
		rows = append(rows, planAutomatonRow{
			Title: a.Title, ID: a.ID, State: a.State, Run: a.Run, Position: a.Position,
			CompletedAt: a.CompletedAt, Points: points[a.ID],
			Skipped: a.Run == planrecords.RunBlocked || a.Run == planrecords.RunHeld,
			IsNext:  d.Next != nil && d.Next.ID == a.ID,
		})
	}
	var next *planNextMarker
	if d.Next != nil {
		next = &planNextMarker{AutomatonID: d.Next.ID, Title: d.Next.Title}
	}
	writeJSONOK(w, map[string]any{
		"plan":     map[string]any{"title": d.Summary.Title, "id": d.Summary.ID, "state": string(d.Summary.State), "set_by_hand": d.Summary.SetByHand},
		"progress": newPlanProgress(d.Done, len(d.Automata)),
		"next":     next,
		"automata": rows,
		"page":     pageMeta(pg, total),
	})
}

// planTaskRow is one task of a plan's Automata.
type planTaskRow struct {
	Title         string `json:"title"`
	ID            string `json:"id"`
	Status        string `json:"status"`
	AutomatonID   string `json:"automaton_id"`
	AutomatonName string `json:"automaton_title,omitempty"`
	StoryTitle    string `json:"story_title,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"`
}

// planTasksOf lists the tasks of one Automaton. A test sets planTasksSource;
// otherwise it reads the Automaton record, which only the tasks route does.
func (s *Server) planTasksOf(automatonID string) ([]planTaskRow, bool) {
	if s.planTasksSource != nil {
		return s.planTasksSource(automatonID)
	}
	if s.autonomousMgr == nil {
		return nil, false
	}
	raw, ok := s.autonomousMgr.GetPRD(automatonID)
	if !ok {
		return nil, false
	}
	p, err := toPRD(raw)
	if err != nil || p == nil {
		return nil, false
	}
	var out []planTaskRow
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			row := planTaskRow{
				Title: t.Title, ID: t.ID, Status: string(t.Status), AutomatonID: p.ID,
				AutomatonName: p.Title, StoryTitle: st.Title,
			}
			if t.CompletedAt != nil {
				row.CompletedAt = t.CompletedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, row)
		}
	}
	return out, true
}

// handlePlanTasks serves GET /api/plans/{ref}/tasks. Call it when a plan is
// expanded; the plans and Automata lists do not carry tasks.
func (s *Server) handlePlanTasks(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	pg, ok := pageParams(w, r, "")
	if !ok {
		return
	}
	sum, order, err := s.planRecords.PlanAutomatonIDs(ref)
	if err != nil {
		writePlanRecordStoreError(w, err, "read the plan")
		return
	}
	var all []planTaskRow
	for _, a := range order {
		if tasks, ok := s.planTasksOf(a.ID); ok {
			all = append(all, tasks...)
		}
	}
	lo, hi := pageWindow(pg, len(all))
	if all == nil {
		all = []planTaskRow{}
	}
	writeJSONOK(w, map[string]any{
		"plan":  map[string]any{"title": sum.Title, "id": sum.ID, "state": string(sum.State), "set_by_hand": sum.SetByHand},
		"tasks": all[lo:hi],
		"page":  pageMeta(pg, len(all)),
	})
}

// pageWindow turns a Page into slice bounds over total items.
func pageWindow(pg planrecords.Page, total int) (int, int) {
	lim := pg.Limit
	if lim <= 0 {
		lim = planrecords.DefaultLimit
	}
	if lim > planrecords.MaxLimit {
		lim = planrecords.MaxLimit
	}
	lo := pg.Offset
	if lo > total {
		lo = total
	}
	hi := lo + lim
	if hi > total {
		hi = total
	}
	return lo, hi
}

// browseRow is one registry row or finding of the browse list.
type browseRow struct {
	Title        string   `json:"title"`
	ID           string   `json:"id"`
	UID          string   `json:"uid"`
	Kind         string   `json:"kind"`
	Project      string   `json:"project"`
	State        string   `json:"state"`
	LegacyNumber int      `json:"legacy_number,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
	PlanID       string   `json:"plan_id,omitempty"`
	PlanTitle    string   `json:"plan_title,omitempty"`
	AutomatonIDs []string `json:"automaton_ids"`
}

// handlePlanRecordBrowse serves GET /api/plan-records/browse.
func (s *Server) handlePlanRecordBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pg, ok := pageParams(w, r, "")
	if !ok {
		return
	}
	f := planrecords.BrowseFilter{
		Query: strings.TrimSpace(q.Get("q")), Project: strings.TrimSpace(q.Get("project")), Page: pg,
	}
	if v := q.Get("kind"); v != "" {
		k := planrecords.Kind(v)
		if k != planrecords.KindRegistryRow && k != planrecords.KindFinding {
			planRecordsError(w, http.StatusBadRequest, `kind must be "registry_row" or "finding"`)
			return
		}
		f.Kind = k
	}
	if v := q.Get("state"); v != "" {
		st := planrecords.State(v)
		if (f.Kind == planrecords.KindFinding || !planrecords.ValidState(planrecords.KindRegistryRow, st)) &&
			(f.Kind == planrecords.KindRegistryRow || !planrecords.ValidState(planrecords.KindFinding, st)) {
			planRecordsError(w, http.StatusBadRequest, "state "+strconv.Quote(v)+" is not a valid state for this kind")
			return
		}
		f.State = st
	}
	rows, total := s.planRecords.Browse(f)
	out := make([]browseRow, 0, len(rows))
	for _, b := range rows {
		sm := b.Summary
		out = append(out, browseRow{
			Title: sm.Title, ID: sm.ID, UID: sm.UID, Kind: string(sm.Kind), Project: sm.Project,
			State: string(sm.State), LegacyNumber: sm.LegacyNumber, UpdatedAt: sm.UpdatedAt,
			PlanID: b.PlanID, PlanTitle: b.PlanName, AutomatonIDs: b.Automata,
		})
	}
	writeJSONOK(w, map[string]any{"records": out, "page": pageMeta(pg, total)})
}

// handleProjectArcs serves GET /api/projects/{name}/arcs, kept as a deprecated
// alias of the roll-up arcs: the same arcs data, plus the plans.
func (s *Server) handleProjectArcs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
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
	roll, ok := s.projectRollupFor(w, r, name, rollupDefaultWindowDays)
	if !ok {
		return
	}
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Link", `</api/projects/`+name+`/plans>; rel="successor-version"`)
	writeJSONOK(w, map[string]any{
		"deprecated": "Use GET /api/projects/{name}/plans. This route returns the roll-up arcs plus plans.",
		"project":    name,
		"arcs":       roll.Arcs,
		"plans":      s.plansForAlias(name),
	})
}

// plansForAlias is the plans block the deprecated routes add: the first page of
// the project's plans from the index, or an empty list while plan records are
// off or not wired.
func (s *Server) plansForAlias(project string) []planRow {
	out := []planRow{}
	if s.planRecords == nil || !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		return out
	}
	view := s.planRecords.ProjectPlans(project, planrecords.Page{}, planrecords.Page{})
	points := s.automatonPoints()
	for _, d := range append(append([]planrecords.PlanDigest{}, view.Plans...), view.Backlog...) {
		out = append(out, s.planRowFor(d, points))
	}
	return out
}
