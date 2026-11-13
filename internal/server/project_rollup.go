// Project roll-up read: GET /api/projects/{name}/rollup.
//
// The roll-up answers "how far along is this project" from the records the
// daemon already keeps. It reads the project's arcs (ordered Automation slots,
// config.ProjectArc), the Automata (PRDs) those Automations hold, each Automaton's
// task progress, its cost in usage points and the throughput over a window. No
// storage is added: the Automaton store, the Automation records and the cost
// report engine are the only inputs.
//
// Grouping: an Automaton belongs to the arc whose Automation holds it, found by
// PRD.AutomationID or, failing that, the Automation whose members include it.
// An Automaton that no arc holds goes in the "unassigned" group when it
// resolves to this project. Automata resolved to another project are left out,
// even when their directory matches this project's directory. Automata that
// resolve to no project and sit in no project's arc go in the separate
// "without_project" group (Automata without a project), which is built from the
// full Automaton list, is the same whichever project is asked for, and is not
// counted in any Total.
//
// Cost: points only, never API dollars. Each Automaton's points are the
// cost report's estimated_usage_points for its group_by=automaton row, which
// the report derives from the billing profile's point-cost calibration
// (PointCostEquivalent). An Automaton whose backends are not on a bundled
// plan has no point figure and reads as 0; points_priced says whether any
// Automaton was priced.
//
// Membership: an Automaton is placed in a project by autonomous.ResolveAutomatonProject,
// in this order: the Automaton's own project link, the project of the Automation
// that holds it, the project of its parent Automaton, then its directory (a
// worktree maps to its repository). The arc that holds an Automaton still
// places it in that arc.
//
// Throughput: an Automaton counts as finished when it is completed, or when it
// is cancelled or archived and its work branch is already on the project's
// default branch (the read-only merge proof the merged-member decision uses),
// or when it carries a shipped record (a merge commit). The supervisor and the
// daemon cancel and archive an Automaton right after its work is merged, so a
// status of completed alone would read shipped work as zero. The finish time is
// the merge or ship time when known, else the last update, and it must fall in
// the window. A cancelled Automaton whose branch is not merged is not counted.
// A task counts from its own CompletedAt when it is completed and that time
// falls in the window, for every Automaton of the project whatever its final
// status, and never twice. Both are divided by the window length in days.
//
// Query: window=<days> (1–365, default 7) sets the throughput window and the
// cost window. Reads require CapAutonomousRead, as the other project reads do.

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	rollupDefaultWindowDays = 7
	rollupMaxWindowDays     = 365
)

// rollupTasks is task progress: how many are done out of how many.
type rollupTasks struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// rollupThroughput is what finished in the window and the per-day rate.
type rollupThroughput struct {
	AutomataFinished int     `json:"automata_finished"`
	TasksFinished    int     `json:"tasks_finished"`
	AutomataPerDay   float64 `json:"automata_per_day"`
	TasksPerDay      float64 `json:"tasks_per_day"`
}

// rollupAutomaton is one Automaton line of a group.
type rollupAutomaton struct {
	ID           string      `json:"id"`
	Title        string      `json:"title,omitempty"`
	Status       string      `json:"status"`
	AutomationID string      `json:"automation_id,omitempty"`
	Finished     bool        `json:"finished"`
	Tasks        rollupTasks `json:"tasks"`
	Points       float64     `json:"points"`
	SessionIDs   []string    `json:"session_ids"`
}

// rollupGroup is one arc, the unassigned bucket, or the whole-project total.
// The arc fields are set only on arc groups.
type rollupGroup struct {
	AutomationID string            `json:"automation_id,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	State        string            `json:"state,omitempty"`
	Automata     []rollupAutomaton `json:"automata"`
	Tasks        rollupTasks       `json:"tasks"`
	Points       float64           `json:"points"`
	Throughput   rollupThroughput  `json:"throughput"`
}

// projectRollup is the GET /api/projects/{name}/rollup response.
type projectRollup struct {
	Project      string        `json:"project"`
	WindowDays   int           `json:"window_days"`
	GeneratedAt  time.Time     `json:"generated_at"`
	PointsPriced bool          `json:"points_priced"`
	Arcs         []rollupGroup `json:"arcs"`
	Unassigned   rollupGroup   `json:"unassigned"`
	// WithoutProject lists the Automata without a project: no link, no
	// Automation or parent project, no directory match and no arc. It is
	// not part of Total.
	WithoutProject rollupGroup `json:"without_project"`
	Total          rollupGroup `json:"total"`
}

// rollupInput is everything buildProjectRollup reads. It holds plain records
// so the grouping and arithmetic can be tested without a running daemon.
type rollupInput struct {
	Project string
	Dir     string
	Arcs    []config.ProjectArc
	// Entries are the Automaton summary-index entries. Session ids, task
	// counts, finish time, shipped state and project link all come from them.
	Entries     []autonomous.SummaryEntry
	Automations []*autonomous.Automation
	// ExtraSessionIDs optionally adds live session ids that are not in the
	// entry (the live planning sessions). Nil adds none.
	ExtraSessionIDs func(prdID string) []string
	// Points maps an Automaton id to its points. An Automaton with no entry is unpriced.
	Points     map[string]float64
	WindowDays int
	Now        time.Time
	// Projects maps every configured project name to its directory, for the
	// membership resolver. Nil means only this project (Project -> Dir).
	Projects map[string]string
	// ArcHeld is the set of Automation ids held by an arc of any configured
	// project. An Automaton in it is never listed as without a project.
	ArcHeld map[string]bool
	// Merged is the read-only merge proof: whether the Automaton's work branch
	// is already on the project's default branch and, when known, when it
	// landed (zero time = unknown). It is asked only for a cancelled or
	// archived Automaton with no shipped record. Nil means not merged.
	Merged func(e autonomous.SummaryEntry) (merged bool, at time.Time)

	// seenTasks keeps a task from being counted twice in the throughput.
	seenTasks map[string]bool
}

// parseRollupWindow reads the window query value. Empty means the default.
func parseRollupWindow(raw string) (int, error) {
	if raw == "" {
		return rollupDefaultWindowDays, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > rollupMaxWindowDays {
		return 0, fmt.Errorf("window must be a whole number of days from 1 to %d", rollupMaxWindowDays)
	}
	return n, nil
}

// buildProjectRollup groups the project's Automata by arc and sums them.
func buildProjectRollup(in rollupInput) projectRollup {
	in.seenTasks = map[string]bool{}
	since := in.Now.Add(-time.Duration(in.WindowDays) * 24 * time.Hour)
	inWindow := func(t *time.Time) bool {
		return t != nil && !t.Before(since) && !t.After(in.Now)
	}

	// Which automation holds each Automaton: PRD.AutomationID first, else
	// the first Automation (in list order) with it as a member.
	memberOf := map[string]string{}
	for _, a := range in.Automations {
		if a == nil {
			continue
		}
		for _, m := range a.Members {
			if m.PRDID == "" {
				continue
			}
			if _, ok := memberOf[m.PRDID]; !ok {
				memberOf[m.PRDID] = a.ID
			}
		}
	}

	arcIndex := map[string]int{}
	out := projectRollup{Project: in.Project, WindowDays: in.WindowDays, GeneratedAt: in.Now}
	out.Arcs = make([]rollupGroup, 0, len(in.Arcs))
	for _, arc := range in.Arcs {
		if _, dup := arcIndex[arc.AutomationID]; dup {
			continue
		}
		arcIndex[arc.AutomationID] = len(out.Arcs)
		state := arc.State
		if state == "" {
			state = config.ArcStatePlanned
		}
		out.Arcs = append(out.Arcs, rollupGroup{
			AutomationID: arc.AutomationID, Priority: arc.Priority, State: state,
			Automata: []rollupAutomaton{},
		})
	}
	out.Unassigned = rollupGroup{Automata: []rollupAutomaton{}}

	projects := in.Projects
	if projects == nil {
		projects = map[string]string{in.Project: in.Dir}
	}
	idx := autonomous.NewAutomatonProjectIndexFromSummary(in.Entries, in.Automations, projects)
	out.WithoutProject = rollupGroup{Automata: []rollupAutomaton{}}
	seenPRD := map[string]bool{}
	for i := range in.Entries {
		p := &in.Entries[i]
		if p.IsTemplate || seenPRD[p.ID] {
			continue
		}
		auto := p.AutomationID
		if auto == "" {
			auto = memberOf[p.ID]
		}
		resolved := idx.Of(p.ID).Project
		var g *rollupGroup
		if i, ok := arcIndex[auto]; ok && auto != "" {
			g = &out.Arcs[i]
		} else if resolved == in.Project {
			g = &out.Unassigned
		} else if resolved == "" && (auto == "" || !in.ArcHeld[auto]) {
			g = &out.WithoutProject
		} else {
			continue
		}
		seenPRD[p.ID] = true
		g.Automata = append(g.Automata, rollupAutomatonFor(p, auto, in, inWindow, g))
	}

	for i := range out.Arcs {
		finishGroup(&out.Arcs[i], in.WindowDays)
	}
	finishGroup(&out.Unassigned, in.WindowDays)
	finishGroup(&out.WithoutProject, in.WindowDays)

	out.Total = rollupGroup{Automata: []rollupAutomaton{}}
	for _, g := range append(append([]rollupGroup{}, out.Arcs...), out.Unassigned) {
		out.Total.Automata = append(out.Total.Automata, g.Automata...)
		out.Total.Tasks.Done += g.Tasks.Done
		out.Total.Tasks.Total += g.Tasks.Total
		out.Total.Points += g.Points
		out.Total.Throughput.AutomataFinished += g.Throughput.AutomataFinished
		out.Total.Throughput.TasksFinished += g.Throughput.TasksFinished
	}
	setPerDay(&out.Total.Throughput, in.WindowDays)

	out.PointsPriced = len(in.Points) > 0
	return out
}

// rollupAutomatonFor builds one Automaton line and adds its finished tasks to
// the group's throughput. The group's task and finished counts are set here so
// finishGroup only has to sum points and divide.
func rollupAutomatonFor(p *autonomous.SummaryEntry, auto string, in rollupInput, inWindow func(*time.Time) bool, g *rollupGroup) rollupAutomaton {
	ra := rollupAutomaton{
		ID: p.ID, Title: p.Title, Status: string(p.Status),
		AutomationID: auto, SessionIDs: []string{},
	}
	if ids := rollupSessionIDs(p, in.ExtraSessionIDs); ids != nil {
		ra.SessionIDs = ids
	}
	finished, finishedAt := rollupFinished(p, in)
	ra.Finished = finished
	if finished && !finishedAt.IsZero() && inWindow(&finishedAt) {
		g.Throughput.AutomataFinished++
	}
	for _, t := range p.Tasks {
		ra.Tasks.Total++
		g.Tasks.Total++
		if t.Status == autonomous.TaskCompleted {
			ra.Tasks.Done++
			g.Tasks.Done++
			key := p.ID + "/" + t.ID
			if inWindow(t.CompletedAt) && !in.seenTasks[key] {
				in.seenTasks[key] = true
				g.Throughput.TasksFinished++
			}
		}
	}
	if pts, ok := in.Points[p.ID]; ok {
		ra.Points = pts
	}
	return ra
}

// rollupFinished decides whether an Automaton counts as finished and when it
// finished: completed; or cancelled or archived with its branch already on the
// default branch; or carrying a shipped record. The time is the merge or ship
// time when known, else the last update.
func rollupFinished(p *autonomous.SummaryEntry, in rollupInput) (bool, time.Time) {
	at := func(known time.Time) time.Time {
		if !known.IsZero() {
			return known
		}
		return p.UpdatedAt
	}
	var shipAt time.Time
	if p.MergedAt != nil {
		shipAt = *p.MergedAt
	}
	switch {
	case p.IsShipped():
		return true, at(shipAt)
	case p.Status == autonomous.PRDCompleted:
		return true, at(shipAt)
	case (p.Status == autonomous.PRDCancelled || p.Status == autonomous.PRDArchived) && in.Merged != nil:
		if merged, mergedAt := in.Merged(*p); merged {
			return true, at(mergedAt)
		}
	}
	return false, time.Time{}
}

// rollupSessionIDs is the Automaton's session ids: the entry's cost sessions
// in cost roll-up order, then any live ones the extra lookup names, without
// duplicates. It returns nil when there are none.
func rollupSessionIDs(p *autonomous.SummaryEntry, extra func(string) []string) []string {
	var live []string
	if extra != nil {
		live = extra(p.ID)
	}
	if len(live) == 0 {
		if len(p.SessionIDs) == 0 {
			return nil
		}
		return append([]string(nil), p.SessionIDs...)
	}
	seen := make(map[string]struct{}, len(p.SessionIDs)+len(live))
	out := make([]string, 0, len(p.SessionIDs)+len(live))
	for _, id := range append(append([]string(nil), p.SessionIDs...), live...) {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// finishGroup sums the points and turns the window counts into per-day rates.
func finishGroup(g *rollupGroup, windowDays int) {
	for _, a := range g.Automata {
		g.Points += a.Points
	}
	setPerDay(&g.Throughput, windowDays)
}

func setPerDay(t *rollupThroughput, windowDays int) {
	days := float64(windowDays)
	t.AutomataPerDay = float64(t.AutomataFinished) / days
	t.TasksPerDay = float64(t.TasksFinished) / days
}

// handleProjectRollup serves GET /api/projects/{name}/rollup. It reads only the
// summary index; a repeat read with an unchanged key is served from the
// per-project cache (project_rollup_cache.go).
func (s *Server) handleProjectRollup(w http.ResponseWriter, r *http.Request) {
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
	windowDays, err := parseRollupWindow(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		http.Error(w, "automations not available", http.StatusServiceUnavailable)
		return
	}
	body := s.projectRollupBody(mgr, name, entry, windowDays)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// rollupProjectDirs is the configured project name to directory map the
// membership resolver reads.
func rollupProjectDirs(projects map[string]config.ProjectConfigEntry) map[string]string {
	out := make(map[string]string, len(projects))
	for name, e := range projects {
		out[name] = e.Dir
	}
	return out
}

// rollupArcHeld is the set of Automation ids held by an arc of any project.
func rollupArcHeld(projects map[string]config.ProjectConfigEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range projects {
		for _, arc := range e.Arcs {
			if arc.AutomationID != "" {
				out[arc.AutomationID] = true
			}
		}
	}
	return out
}
